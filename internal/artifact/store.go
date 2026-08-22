// Package artifact defines the storage boundary for immutable content.
//
// Parts and events carry references and metadata, never the artifact bytes.
// The Store interface is deliberately small so a local filesystem backend,
// an S3-compatible backend, or another durable object store can be selected
// without changing the Part model.
package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	refPrefix = "artifact:sha256:"
	refSize   = len(refPrefix) + sha256.Size*2
)

var (
	ErrInvalidRef       = errors.New("invalid artifact reference")
	ErrMetadataMissing  = errors.New("artifact metadata is missing")
	ErrMetadataMismatch = errors.New("artifact metadata does not match content")
	ErrPathTooLong      = errors.New("artifact path is too long")
)

// Ref is a canonical content-addressed artifact reference. Its only accepted
// form in the first backend is artifact:sha256:<lowercase hex digest>.
type Ref string

func (r Ref) String() string { return string(r) }

// ParseRef validates and canonicalizes an artifact reference.
func ParseRef(raw string) (Ref, error) {
	if len(raw) != refSize || !strings.HasPrefix(raw, refPrefix) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, raw)
	}
	digest := raw[len(refPrefix):]
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(digest) != digest {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, raw)
	}
	return Ref(raw), nil
}

func newRef(sum [sha256.Size]byte) Ref {
	return Ref(refPrefix + hex.EncodeToString(sum[:]))
}

// Metadata is the durable description of an immutable artifact. MediaType is
// descriptive metadata; it does not determine how a renderer or processor
// handles the bytes.
type Metadata struct {
	Ref       Ref    `json:"ref"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type,omitempty"`
	Size      int64  `json:"size"`
}

// Store is the storage-neutral artifact contract. Implementations must make
// Put durable before returning and must not expose partially written bytes.
type Store interface {
	Put(ctx context.Context, content io.Reader, mediaType string) (Metadata, error)
	Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
	Stat(ctx context.Context, ref Ref) (Metadata, error)
	Exists(ctx context.Context, ref Ref) (bool, error)
}

// LocalStore is a private, content-addressed filesystem backend. It stores
// bytes and a JSON metadata sidecar under root. This is the embedded/MVP
// backend; remote object-store adapters can implement Store later.
type LocalStore struct {
	root string
}

// Object is one filesystem object in the local CAS. Valid is false when the
// data or metadata is missing/corrupt; Err describes the validation failure.
// Maintenance tools use this rather than treating an invalid object as an
// absent object.
type Object struct {
	Ref          Ref
	Metadata     Metadata
	DataPath     string
	MetadataPath string
	ModifiedAt   time.Time
	Valid        bool
	Err          error
}

func NewLocalStore(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("artifact store root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, wrapPathError(root, err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, wrapPathError(root, err)
	}
	// Materialize the fixed CAS directory shape once so an unusable configured
	// root fails at open time, before a caller can believe an upload succeeded.
	layout := filepath.Join(root, "sha256", "00", "00")
	if err := os.MkdirAll(layout, 0o700); err != nil {
		return nil, wrapPathError(layout, err)
	}
	if err := os.Chmod(layout, 0o700); err != nil {
		return nil, wrapPathError(layout, err)
	}
	return &LocalStore{root: root}, nil
}

func (s *LocalStore) Root() string { return s.root }

func (s *LocalStore) Put(ctx context.Context, content io.Reader, mediaType string) (Metadata, error) {
	if content == nil {
		return Metadata{}, errors.New("artifact content is nil")
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	tmp, err := os.CreateTemp(s.root, ".artifact-*tmp")
	if err != nil {
		return Metadata{}, wrapPathError(s.root, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return Metadata{}, wrapPathError(tmpName, err)
	}
	hasher := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(tmp, hasher), content)
	if copyErr != nil {
		tmp.Close()
		return Metadata{}, copyErr
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return Metadata{}, wrapPathError(tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return Metadata{}, wrapPathError(tmpName, err)
	}

	var sum [sha256.Size]byte
	copy(sum[:], hasher.Sum(nil))
	ref := newRef(sum)
	dataPath, metadataPath, err := s.paths(ref)
	if err != nil {
		return Metadata{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o700); err != nil {
		return Metadata{}, wrapPathError(dataPath, err)
	}
	if existing, statErr := os.Stat(dataPath); statErr == nil {
		if existing.Size() != written {
			return Metadata{}, fmt.Errorf("%w: existing size %d, new size %d", ErrMetadataMismatch, existing.Size(), written)
		}
	} else if os.IsNotExist(statErr) {
		if err := os.Rename(tmpName, dataPath); err != nil {
			return Metadata{}, wrapPathError(dataPath, err)
		}
		if err := syncDir(filepath.Dir(dataPath)); err != nil {
			return Metadata{}, wrapPathError(filepath.Dir(dataPath), err)
		}
	} else {
		return Metadata{}, wrapPathError(dataPath, statErr)
	}

	metadata := Metadata{
		Ref:       ref,
		Algorithm: "sha256",
		Digest:    hex.EncodeToString(sum[:]),
		MediaType: strings.TrimSpace(mediaType),
		Size:      written,
	}
	if existing, err := readMetadata(metadataPath); err == nil {
		if err := validateMetadata(existing, ref, written); err != nil {
			return Metadata{}, err
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Metadata{}, err
	}
	if err := writeMetadata(metadataPath, metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func (s *LocalStore) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := s.Stat(ctx, ref); err != nil {
		return nil, err
	}
	dataPath, _, err := s.paths(ref)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(dataPath)
	if err != nil {
		return nil, wrapPathError(dataPath, err)
	}
	return file, nil
}

func (s *LocalStore) Stat(ctx context.Context, ref Ref) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	parsed, err := ParseRef(ref.String())
	if err != nil {
		return Metadata{}, err
	}
	dataPath, metadataPath, err := s.paths(parsed)
	if err != nil {
		return Metadata{}, err
	}
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, fmt.Errorf("%w: %s", ErrMetadataMissing, parsed)
		}
		return Metadata{}, err
	}
	fileInfo, err := os.Stat(dataPath)
	if err != nil {
		return Metadata{}, wrapPathError(dataPath, err)
	}
	if err := validateMetadata(metadata, parsed, fileInfo.Size()); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func (s *LocalStore) Exists(ctx context.Context, ref Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := s.Stat(ctx, ref)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if errors.Is(err, ErrMetadataMissing) {
		return false, nil
	}
	return false, err
}

// Scan enumerates the fixed local CAS layout. It returns invalid objects as
// entries with Valid=false so callers can report them separately from safe
// garbage-collection candidates.
func (s *LocalStore) Scan(ctx context.Context) ([]Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Join(s.root, "sha256")
	type paths struct {
		data     string
		metadata string
		modified time.Time
	}
	objects := make(map[string]paths)
	var invalid []Object
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			invalid = append(invalid, Object{DataPath: path, Valid: false, Err: fmt.Errorf("symlink is not allowed in artifact CAS")})
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || len(parts[0]) != 2 || len(parts[1]) != 2 {
			invalid = append(invalid, Object{DataPath: path, ModifiedAt: info.ModTime(), Valid: false, Err: fmt.Errorf("invalid CAS path layout")})
			return nil
		}
		name := parts[2]
		digest := strings.TrimSuffix(name, ".json")
		if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
			invalid = append(invalid, Object{DataPath: path, ModifiedAt: info.ModTime(), Valid: false, Err: fmt.Errorf("invalid CAS digest filename")})
			return nil
		}
		if _, err := hex.DecodeString(digest); err != nil {
			invalid = append(invalid, Object{DataPath: path, ModifiedAt: info.ModTime(), Valid: false, Err: fmt.Errorf("invalid CAS digest filename: %w", err)})
			return nil
		}
		item := objects[digest]
		item.modified = maxTime(item.modified, info.ModTime())
		if strings.HasSuffix(name, ".json") {
			item.metadata = path
		} else {
			item.data = path
		}
		objects[digest] = item
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return append(invalid, nil...), nil
	}
	if err != nil {
		return nil, wrapPathError(root, err)
	}
	result := make([]Object, 0, len(objects)+len(invalid))
	for digest, item := range objects {
		ref, refErr := ParseRef(refPrefix + digest)
		object := Object{Ref: ref, DataPath: item.data, MetadataPath: item.metadata, ModifiedAt: item.modified}
		if refErr != nil {
			object.Valid = false
			object.Err = refErr
		} else if item.data == "" || item.metadata == "" {
			object.Valid = false
			object.Err = ErrMetadataMissing
		} else {
			metadata, statErr := s.Stat(ctx, ref)
			if statErr != nil {
				object.Valid = false
				object.Err = statErr
			} else {
				if verifyErr := s.verifyDigest(ctx, ref, metadata); verifyErr != nil {
					object.Valid = false
					object.Err = verifyErr
				} else {
					object.Valid = true
					object.Metadata = metadata
				}
			}
		}
		result = append(result, object)
	}
	result = append(result, invalid...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Ref.String()
		right := result[j].Ref.String()
		if left == right {
			return result[i].DataPath < result[j].DataPath
		}
		return left < right
	})
	return result, nil
}

// Verify checks the immutable bytes as well as their metadata. Normal reads
// use Stat for speed; maintenance operations use Verify before copying or
// deleting an object.
func (s *LocalStore) Verify(ctx context.Context, ref Ref) (Metadata, error) {
	metadata, err := s.Stat(ctx, ref)
	if err != nil {
		return Metadata{}, err
	}
	if err := s.verifyDigest(ctx, ref, metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func (s *LocalStore) verifyDigest(ctx context.Context, ref Ref, metadata Metadata) error {
	dataPath, _, err := s.paths(ref)
	if err != nil {
		return err
	}
	file, err := os.Open(dataPath)
	if err != nil {
		return wrapPathError(dataPath, err)
	}
	hasher := sha256.New()
	size, copyErr := copyContext(ctx, hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if size != metadata.Size || digest != metadata.Digest {
		return fmt.Errorf("%w: content digest or size differs for %s", ErrMetadataMismatch, ref)
	}
	return nil
}

// Remove deletes one valid or partially written CAS object. It is reserved
// for offline maintenance tools; normal ingestion never removes content.
func (s *LocalStore) Remove(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dataPath, metadataPath, err := s.paths(ref)
	if err != nil {
		return err
	}
	for _, path := range []string{dataPath, metadataPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return wrapPathError(path, err)
		}
	}
	return wrapPathError(filepath.Dir(dataPath), syncDir(filepath.Dir(dataPath)))
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func (s *LocalStore) paths(ref Ref) (dataPath, metadataPath string, err error) {
	parsed, err := ParseRef(ref.String())
	if err != nil {
		return "", "", err
	}
	digest := strings.TrimPrefix(parsed.String(), refPrefix)
	dir := filepath.Join(s.root, "sha256", digest[:2], digest[2:4])
	return filepath.Join(dir, digest), filepath.Join(dir, digest+".json"), nil
}

func readMetadata(path string) (Metadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, wrapPathError(path, err)
	}
	defer file.Close()
	var metadata Metadata
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func validateMetadata(metadata Metadata, ref Ref, size int64) error {
	parsed, err := ParseRef(ref.String())
	if err != nil {
		return err
	}
	if metadata.Ref != parsed || metadata.Algorithm != "sha256" || metadata.Size != size {
		return fmt.Errorf("%w: ref=%q algorithm=%q size=%d", ErrMetadataMismatch, metadata.Ref, metadata.Algorithm, metadata.Size)
	}
	if metadata.Digest != strings.TrimPrefix(parsed.String(), refPrefix) {
		return fmt.Errorf("%w: digest=%q", ErrMetadataMismatch, metadata.Digest)
	}
	return nil
}

func writeMetadata(path string, metadata Metadata) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".metadata-*tmp")
	if err != nil {
		return wrapPathError(filepath.Dir(path), err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return wrapPathError(tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return wrapPathError(tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return wrapPathError(tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return wrapPathError(tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return wrapPathError(path, err)
	}
	return wrapPathError(filepath.Dir(path), syncDir(filepath.Dir(path)))
}

func wrapPathError(path string, err error) error {
	if err == nil {
		return nil
	}
	if isPathTooLong(err) {
		return fmt.Errorf("%w: %s: %v", ErrPathTooLong, path, err)
	}
	return err
}

func isPathTooLong(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "file name too long") ||
		strings.Contains(text, "filename too long") ||
		strings.Contains(text, "path too long") ||
		strings.Contains(text, "name too long")
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	var written int64
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			count, writeErr := dst.Write(buf[:n])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
