package main

import (
	"context"
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

func uploadBackup(ctx context.Context, r Remote, manifest missis.ManifestInfo, src string, force bool) (string, error) {
	key := objectKey(manifest)
	if !force {
		exists, err := r.Exists(ctx, key)
		if err != nil {
			return "", err
		}
		if exists {
			return "", fmt.Errorf("remote backup already exists: %s", key)
		}
	}
	if err := r.Upload(ctx, src, key); err != nil {
		return "", err
	}
	return key, nil
}

func downloadAndVerify(ctx context.Context, r Remote, manifest missis.ManifestInfo, dst string) error {
	key := objectKey(manifest)
	if err := r.Download(ctx, key, dst); err != nil {
		return err
	}
	restored, err := application.OpenPath(dst)
	if err != nil {
		return err
	}
	defer restored.Close()
	got, err := restored.Manifest(ctx)
	if err != nil {
		return err
	}
	if got.StoreID != manifest.StoreID || got.HeadHash != manifest.HeadHash ||
		got.SchemaVersion != manifest.SchemaVersion || got.EventCount != manifest.EventCount {
		return fmt.Errorf("downloaded backup verification failed: store_id %q/%q head %q/%q schema %q/%q events %d/%d",
			got.StoreID, manifest.StoreID, got.HeadHash, manifest.HeadHash,
			got.SchemaVersion, manifest.SchemaVersion, got.EventCount, manifest.EventCount)
	}
	return nil
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
	_, err := os.Stat(r.keyPath(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r *localRemote) Upload(ctx context.Context, src, key string) error {
	dst := r.keyPath(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

func (r *localRemote) Download(ctx context.Context, key, dst string) error {
	return copyFile(r.keyPath(key), dst)
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
	return false, err
}

func (r *awsRemote) Upload(ctx context.Context, src, key string) error {
	return r.run(ctx, "s3", "cp", src, r.target(key))
}

func (r *awsRemote) Download(ctx context.Context, key, dst string) error {
	return r.run(ctx, "s3", "cp", r.target(key), dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
