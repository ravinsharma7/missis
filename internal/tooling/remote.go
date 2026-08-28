package tooling

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

// Remote is the transport abstraction for backup upload/download. The
// orchestration and verification logic lives in this package so it can be
// tested hermetically against a local filesystem remote.
type Remote interface {
	Exists(ctx context.Context, key string) (bool, error)
	Upload(ctx context.Context, src, key string) error
	Download(ctx context.Context, key, dst string) error
}

func resolveRemote() (Remote, error) {
	if dir := os.Getenv("MISSIS_REMOTE_DIR"); dir != "" {
		return &localRemote{dir: dir}, nil
	}
	if os.Getenv("MISSIS_RCLONE_REMOTE") != "" {
		return newRcloneRemote()
	}
	if bucket := os.Getenv("MISSIS_S3_BUCKET"); bucket != "" {
		return &awsRemote{bucket: bucket}, nil
	}
	return nil, fmt.Errorf("no remote upload target configured: set MISSIS_REMOTE_DIR, MISSIS_RCLONE_REMOTE, or MISSIS_S3_BUCKET")
}

// objectKey keeps backups content-addressed by store identity and head hash,
// so a different head can never overwrite an existing backup silently.
func objectKey(manifest missis.ManifestInfo) string {
	return manifest.StoreID + "/" + manifest.HeadHash + ".db"
}

func objectManifestKey(key string) string { return key + ".manifest.json" }

func objectArtifactsKey(key string) string { return key + ".artifacts.tar" }

func objectCompletionKey(key string) string { return key + ".complete.json" }

func uploadBackup(ctx context.Context, r Remote, manifest missis.ManifestInfo, src string, force bool) (string, error) {
	key := objectKey(manifest)
	bundle, err := inspectLocalBackupBundle(src)
	if err != nil {
		return "", err
	}
	if !force {
		exists, err := r.Exists(ctx, key)
		if err != nil {
			return "", err
		}
		if exists {
			return "", fmt.Errorf("remote backup already exists: %s", key)
		}
		if bundle.modern {
			sidecarKeys := []string{objectManifestKey(key), objectArtifactsKey(key)}
			if bundle.completionPath != "" {
				sidecarKeys = append(sidecarKeys, objectCompletionKey(key))
			}
			for _, sidecarKey := range sidecarKeys {
				exists, err := r.Exists(ctx, sidecarKey)
				if err != nil {
					return "", err
				}
				if exists {
					return "", fmt.Errorf("remote backup bundle is incomplete: %s", sidecarKey)
				}
			}
		}
	}
	if err := r.Upload(ctx, src, key); err != nil {
		return "", err
	}
	if bundle.modern {
		if err := r.Upload(ctx, bundle.manifestPath, objectManifestKey(key)); err != nil {
			return "", fmt.Errorf("upload backup manifest: %w", err)
		}
		archivePath, cleanup, err := createArtifactArchive(ctx, bundle.artifactsPath)
		if err != nil {
			return "", err
		}
		defer cleanup()
		if err := r.Upload(ctx, archivePath, objectArtifactsKey(key)); err != nil {
			return "", fmt.Errorf("upload backup artifacts: %w", err)
		}
		if bundle.completionPath != "" {
			if err := r.Upload(ctx, bundle.completionPath, objectCompletionKey(key)); err != nil {
				return "", fmt.Errorf("upload backup completion marker: %w", err)
			}
		}
	}
	return key, nil
}

func downloadAndVerify(ctx context.Context, r Remote, manifest missis.ManifestInfo, dst string) error {
	key := objectKey(manifest)
	if err := r.Download(ctx, key, dst); err != nil {
		return err
	}
	hasManifest, err := r.Exists(ctx, objectManifestKey(key))
	if err != nil {
		return err
	}
	hasArtifacts, err := r.Exists(ctx, objectArtifactsKey(key))
	if err != nil {
		return err
	}
	hasCompletion, err := r.Exists(ctx, objectCompletionKey(key))
	if err != nil {
		return err
	}
	if hasManifest != hasArtifacts || (hasCompletion && !hasManifest) {
		return fmt.Errorf("downloaded backup bundle is incomplete: manifest=%v artifacts=%v completion=%v", hasManifest, hasArtifacts, hasCompletion)
	}
	if hasManifest {
		if err := downloadBundleSidecars(ctx, r, key, dst, hasCompletion); err != nil {
			return err
		}
	}
	restored, err := application.OpenPath(dst)
	if err != nil {
		return err
	}
	defer restored.Close()
	return restored.VerifyRestore(ctx, dst, manifest)
}

type localBackupBundle struct {
	modern         bool
	manifestPath   string
	artifactsPath  string
	completionPath string
}

func inspectLocalBackupBundle(src string) (localBackupBundle, error) {
	manifestPath := src + ".manifest.json"
	artifactsPath := src + ".artifacts"
	completionPath := src + ".complete.json"
	manifestInfo, manifestErr := os.Stat(manifestPath)
	artifactsInfo, artifactsErr := os.Stat(artifactsPath)
	completionInfo, completionErr := os.Stat(completionPath)
	if os.IsNotExist(manifestErr) && os.IsNotExist(artifactsErr) {
		if completionErr == nil {
			return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: completion marker exists without sidecars")
		}
		return localBackupBundle{}, nil
	}
	if manifestErr != nil {
		return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: manifest %s: %w", manifestPath, manifestErr)
	}
	if artifactsErr != nil {
		return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: artifacts %s: %w", artifactsPath, artifactsErr)
	}
	if !manifestInfo.Mode().IsRegular() || !artifactsInfo.IsDir() {
		return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: invalid sidecar types for %s", src)
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return localBackupBundle{}, err
	}
	var manifest missis.BackupManifest
	decodeErr := json.NewDecoder(file).Decode(&manifest)
	closeErr := file.Close()
	if decodeErr != nil {
		return localBackupBundle{}, fmt.Errorf("decode backup manifest: %w", decodeErr)
	}
	if closeErr != nil {
		return localBackupBundle{}, closeErr
	}
	if manifest.Version >= missis.BackupManifestVersionV2 {
		if completionErr != nil {
			return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: completion marker %s: %w", completionPath, completionErr)
		}
		if !completionInfo.Mode().IsRegular() {
			return localBackupBundle{}, fmt.Errorf("incomplete backup bundle: invalid completion marker type")
		}
	} else if completionErr != nil && !os.IsNotExist(completionErr) {
		return localBackupBundle{}, completionErr
	}
	bundle := localBackupBundle{modern: true, manifestPath: manifestPath, artifactsPath: artifactsPath}
	if completionErr == nil {
		bundle.completionPath = completionPath
	}
	return bundle, nil
}

func createArtifactArchive(ctx context.Context, root string) (string, func(), error) {
	tmp, err := os.CreateTemp(filepath.Dir(root), ".missis-artifacts-*.tar")
	if err != nil {
		return "", func() {}, err
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	tw := tar.NewWriter(tmp)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact backup contains unsupported symlink: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := copyWithContext(ctx, tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err == nil {
		err = tw.Close()
	} else {
		_ = tw.Close()
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func downloadBundleSidecars(ctx context.Context, r Remote, key, dst string, hasCompletion bool) error {
	dir := filepath.Dir(dst)
	manifestTemp, err := os.CreateTemp(dir, ".missis-manifest-*")
	if err != nil {
		return err
	}
	manifestTempPath := manifestTemp.Name()
	if err := manifestTemp.Close(); err != nil {
		_ = os.Remove(manifestTempPath)
		return err
	}
	defer os.Remove(manifestTempPath)
	if err := r.Download(ctx, objectManifestKey(key), manifestTempPath); err != nil {
		return fmt.Errorf("download backup manifest: %w", err)
	}
	var manifest missis.BackupManifest
	file, err := os.Open(manifestTempPath)
	if err != nil {
		return err
	}
	err = json.NewDecoder(file).Decode(&manifest)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("decode backup manifest: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	if (manifest.Version != missis.BackupManifestVersion && manifest.Version != missis.BackupManifestVersionV2 && manifest.Version != missis.BackupManifestVersionV1) || manifest.ArtifactMode != missis.BackupArtifactEmbedded {
		return fmt.Errorf("unsupported downloaded backup manifest")
	}
	var completionTempPath string
	if manifest.Version >= missis.BackupManifestVersionV2 {
		if !hasCompletion {
			return fmt.Errorf("downloaded backup bundle is incomplete: completion marker is missing")
		}
		completionTemp, err := os.CreateTemp(dir, ".missis-complete-*")
		if err != nil {
			return err
		}
		completionTempPath = completionTemp.Name()
		if err := completionTemp.Close(); err != nil {
			_ = os.Remove(completionTempPath)
			return err
		}
		defer os.Remove(completionTempPath)
		if err := r.Download(ctx, objectCompletionKey(key), completionTempPath); err != nil {
			return fmt.Errorf("download backup completion marker: %w", err)
		}
	}
	archiveTemp, err := os.CreateTemp(dir, ".missis-artifacts-*.tar")
	if err != nil {
		return err
	}
	archivePath := archiveTemp.Name()
	if err := archiveTemp.Close(); err != nil {
		_ = os.Remove(archivePath)
		return err
	}
	defer os.Remove(archivePath)
	if err := r.Download(ctx, objectArtifactsKey(key), archivePath); err != nil {
		return fmt.Errorf("download backup artifacts: %w", err)
	}
	artifactStage, err := os.MkdirTemp(dir, ".missis-artifacts-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(artifactStage)
	if err := extractArtifactArchive(ctx, archivePath, artifactStage); err != nil {
		return err
	}
	finalManifest := dst + ".manifest.json"
	finalArtifacts := dst + ".artifacts"
	finalCompletion := dst + ".complete.json"
	if _, err := os.Stat(finalManifest); err == nil {
		return fmt.Errorf("download destination sidecar already exists: %s", finalManifest)
	}
	if _, err := os.Stat(finalArtifacts); err == nil {
		return fmt.Errorf("download destination sidecar already exists: %s", finalArtifacts)
	}
	if _, err := os.Stat(finalCompletion); err == nil {
		return fmt.Errorf("download destination sidecar already exists: %s", finalCompletion)
	}
	if err := os.Rename(manifestTempPath, finalManifest); err != nil {
		return err
	}
	if err := os.Rename(artifactStage, finalArtifacts); err != nil {
		_ = os.Remove(finalManifest)
		return err
	}
	if completionTempPath != "" {
		if err := os.Rename(completionTempPath, finalCompletion); err != nil {
			_ = os.Remove(finalManifest)
			_ = os.RemoveAll(finalArtifacts)
			return err
		}
	}
	return nil
}

func extractArtifactArchive(ctx context.Context, archivePath, root string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	tr := tar.NewReader(file)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		slashName := filepath.ToSlash(name)
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || (slashName != "sha256" && !strings.HasPrefix(slashName, "sha256/")) {
			return fmt.Errorf("invalid artifact archive path: %q", header.Name)
		}
		target := filepath.Join(root, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := copyWithContext(ctx, out, tr)
			if copyErr == nil {
				copyErr = out.Sync()
			}
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported artifact archive entry %q", header.Name)
		}
	}
}

// localRemote is a filesystem-backed remote used directly when
// MISSIS_REMOTE_DIR is set and as the hermetic test transport.
type localRemote struct {
	dir string
}

func (r *localRemote) keyPath(key string) string {
	// Windows forbids ':' in path segments, and store IDs embed colons
	// (store:01M...). The local filesystem remote sanitizes them; the logical
	// key (used in messages and by rclone/aws) keeps the colon (ticket #55).
	safe := strings.ReplaceAll(key, ":", "_")
	return filepath.Join(r.dir, filepath.FromSlash(safe))
}

func (r *localRemote) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := os.Stat(r.keyPath(key))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r *localRemote) Upload(ctx context.Context, src, key string) error {
	return copyFile(ctx, src, r.keyPath(key))
}

func (r *localRemote) Download(ctx context.Context, key, dst string) error {
	return copyFile(ctx, r.keyPath(key), dst)
}

type rcloneRemote struct {
	remote string
	bucket string
}

func newRcloneRemote() (*rcloneRemote, error) {
	remote := os.Getenv("MISSIS_RCLONE_REMOTE")
	bucket := os.Getenv("MISSIS_RCLONE_BUCKET")
	if remote == "" || bucket == "" {
		return nil, fmt.Errorf("MISSIS_RCLONE_REMOTE and MISSIS_RCLONE_BUCKET are required")
	}
	return &rcloneRemote{remote: remote, bucket: bucket}, nil
}

func (r *rcloneRemote) configPath() (string, error) {
	config := fmt.Sprintf(`[missis]
type = s3
provider = Cloudflare
access_key_id = %s
secret_access_key = %s
endpoint = %s
region = auto
`, os.Getenv("RCLONE_CONFIG_MISSIS_ACCESS_KEY_ID"),
		os.Getenv("RCLONE_CONFIG_MISSIS_SECRET_ACCESS_KEY"),
		os.Getenv("RCLONE_CONFIG_MISSIS_ENDPOINT"))
	file, err := os.CreateTemp("", "rclone-*.conf")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.WriteString(config); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func (r *rcloneRemote) root() string {
	remotePath := strings.TrimSuffix(r.remote, "/")
	if strings.HasSuffix(remotePath, ":") {
		return remotePath + r.bucket
	}
	return remotePath
}

func (r *rcloneRemote) target(key string) string {
	return r.root() + "/" + key
}

func (r *rcloneRemote) run(ctx context.Context, args ...string) error {
	config, err := r.configPath()
	if err != nil {
		return err
	}
	defer os.Remove(config)
	cmd := exec.CommandContext(ctx, "rclone", append([]string{"--config", config}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rclone %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (r *rcloneRemote) Exists(ctx context.Context, key string) (bool, error) {
	err := r.run(ctx, "size", r.target(key))
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "directory not found") || strings.Contains(err.Error(), "error listing") {
		return false, nil
	}
	return false, err
}

func (r *rcloneRemote) Upload(ctx context.Context, src, key string) error {
	return r.run(ctx, "--s3-no-check-bucket", "copyto", src, r.target(key))
}

func (r *rcloneRemote) Download(ctx context.Context, key, dst string) error {
	return r.run(ctx, "--s3-no-check-bucket", "copyto", r.target(key), dst)
}

type awsRemote struct {
	bucket string
}

func (r *awsRemote) target(key string) string {
	return "s3://" + r.bucket + "/missis-backups/" + key
}

func (r *awsRemote) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "aws", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (r *awsRemote) Exists(ctx context.Context, key string) (bool, error) {
	err := r.run(ctx, "s3", "ls", r.target(key))
	if err == nil {
		return true, nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "nosuchkey") || strings.Contains(lower, "not exist") || strings.Contains(lower, "404") {
		return false, nil
	}
	return false, err
}

func (r *awsRemote) Upload(ctx context.Context, src, key string) error {
	return r.run(ctx, "s3", "cp", src, r.target(key))
}

func (r *awsRemote) Download(ctx context.Context, key, dst string) error {
	return r.run(ctx, "s3", "cp", r.target(key), dst)
}

func copyFile(ctx context.Context, src, dst string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := in.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := copyWithContext(ctx, tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
