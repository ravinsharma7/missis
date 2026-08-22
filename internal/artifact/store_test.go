package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorePutStatOpenIsContentAddressed(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	want := "hello artifact\n"
	metadata, err := store.Put(context.Background(), strings.NewReader(want), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(metadata.Ref.String(), "artifact:sha256:") {
		t.Fatalf("ref = %q", metadata.Ref)
	}
	if metadata.Size != int64(len(want)) || metadata.MediaType != "text/plain" {
		t.Fatalf("metadata = %+v", metadata)
	}
	gotMetadata, err := store.Stat(context.Background(), metadata.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if gotMetadata != metadata {
		t.Fatalf("stat = %+v, put = %+v", gotMetadata, metadata)
	}
	reader, err := store.Open(context.Background(), metadata.Ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	exists, err := store.Exists(context.Background(), metadata.Ref)
	if err != nil || !exists {
		t.Fatalf("exists = %v, %v", exists, err)
	}

	second, err := store.Put(context.Background(), strings.NewReader(want), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if second != metadata {
		t.Fatalf("repeat put changed durable metadata: first=%+v second=%+v", metadata, second)
	}

	digest := strings.TrimPrefix(metadata.Ref.String(), refPrefix)
	dataPath := filepath.Join(store.Root(), "sha256", digest[:2], digest[2:4], digest)
	metadataPath := dataPath + ".json"
	for _, path := range []string{dataPath, metadataPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func TestLocalStoreRejectsInvalidAndMissingReferences(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRef("https://example.test/blob"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("invalid ref error = %v", err)
	}
	missing := Ref("artifact:sha256:" + strings.Repeat("0", 64))
	if _, err := store.Stat(context.Background(), missing); !errors.Is(err, ErrMetadataMissing) {
		t.Fatalf("missing stat error = %v", err)
	}
	exists, err := store.Exists(context.Background(), missing)
	if err != nil || exists {
		t.Fatalf("missing exists = %v, %v", exists, err)
	}
}

func TestLocalStorePutHonorsCancellation(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, strings.NewReader("not stored"), "text/plain"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled put error = %v", err)
	}
}

func TestLocalStoreRejectsOverlongRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("x", 300))
	_, err := NewLocalStore(root)
	if !errors.Is(err, ErrPathTooLong) {
		t.Fatalf("overlong root error = %v, want ErrPathTooLong", err)
	}
}

func TestLocalStoreScanValidatesContentAndMetadata(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(context.Background(), strings.NewReader("scan me"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || !objects[0].Valid || objects[0].Metadata != metadata {
		t.Fatalf("scan = %+v, want one valid object %+v", objects, metadata)
	}

	digest := strings.TrimPrefix(metadata.Ref.String(), refPrefix)
	dataPath := filepath.Join(store.Root(), "sha256", digest[:2], digest[2:4], digest)
	if err := os.WriteFile(dataPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	objects, err = store.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Valid || !errors.Is(objects[0].Err, ErrMetadataMismatch) {
		t.Fatalf("tampered scan = %+v, want invalid metadata mismatch", objects)
	}
}

func TestLocalStoreScanReportsIncompleteObjectAndRemoveIsComplete(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(context.Background(), strings.NewReader("remove me"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.TrimPrefix(metadata.Ref.String(), refPrefix)
	metadataPath := filepath.Join(store.Root(), "sha256", digest[:2], digest[2:4], digest+".json")
	if err := os.Remove(metadataPath); err != nil {
		t.Fatal(err)
	}
	objects, err := store.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Valid || !errors.Is(objects[0].Err, ErrMetadataMissing) {
		t.Fatalf("incomplete scan = %+v, want missing metadata", objects)
	}
	if err := store.Remove(context.Background(), metadata.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(metadataPath)); err != nil {
		t.Fatal(err)
	}
}
