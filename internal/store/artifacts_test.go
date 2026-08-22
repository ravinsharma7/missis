package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactMetadataIndexIsDurableAndDoesNotStoreBytes(t *testing.T) {
	// covers N104
	path := filepath.Join(t.TempDir(), "missis.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := "artifact:sha256:" + strings.Repeat("b", 64)
	record := ArtifactRecord{
		Ref:        ref,
		Algorithm:  "sha256",
		Digest:     strings.Repeat("b", 64),
		MediaType:  "image/png",
		Size:       42,
		Backend:    "local",
		RecordedAt: time.Date(2026, 8, 22, 1, 2, 3, 0, time.UTC),
	}
	if err := s.RecordArtifact(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordArtifact(context.Background(), record); err != nil {
		t.Fatalf("idempotent record: %v", err)
	}
	got, err := s.GetArtifact(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != record.Ref || got.Size != record.Size || got.MediaType != record.MediaType || got.Backend != record.Backend {
		t.Fatalf("record = %+v, want %+v", got, record)
	}
	if _, err := s.GetArtifact(context.Background(), ref+"x"); err == nil {
		t.Fatal("invalid artifact reference was accepted")
	}
	if _, err := s.ListArtifacts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.GetArtifact(context.Background(), ref)
	if err != nil || got.Ref != ref {
		t.Fatalf("reopened record = %+v, %v", got, err)
	}
}

func TestArtifactMetadataIndexRejectsConflicts(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "missis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	record := ArtifactRecord{
		Ref:       "artifact:sha256:" + strings.Repeat("c", 64),
		Digest:    strings.Repeat("c", 64),
		MediaType: "text/plain",
		Size:      4,
		Backend:   "local",
	}
	if err := s.RecordArtifact(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	record.Size = 5
	if err := s.RecordArtifact(context.Background(), record); !errors.Is(err, ErrArtifactConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}
