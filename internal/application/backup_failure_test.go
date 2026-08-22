package application

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestCanceledBackupLeavesNoPublishedBundleOrTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	artifactRoot := filepath.Join(dir, "artifacts")
	svc, err := OpenPathWithClockAndArtifactRoot(filepath.Join(dir, "missis.db"), fixedClock{fixedNow()}, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	created, err := svc.NewTicket(context.Background(), missis.RequestContext{}, missis.NewTicketOptions{Title: "cancel backup"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(context.Background(), missis.RequestContext{}, missis.IngestOptions{
		Target: created.ID, MediaType: "application/octet-stream", SourceName: "payload.bin", Content: strings.NewReader("payload"),
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	svc.artifacts = &blockingArtifactStore{Store: svc.artifacts, started: started}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backup := filepath.Join(dir, "backup.db")
	result := make(chan error, 1)
	go func() { result <- svc.BackupTo(ctx, backup) }()
	<-started
	cancel()
	if err := <-result; err == nil {
		t.Fatal("expected canceled backup to fail")
	}
	for _, path := range []string{backup, backup + ".manifest.json", backup + ".artifacts"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("published path %s exists after canceled backup: %v", path, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".backup.db") {
			t.Fatalf("temporary backup file remains after cancellation: %s", entry.Name())
		}
	}
}

type blockingArtifactStore struct {
	artifact.Store
	started chan struct{}
	once    sync.Once
}

func (s *blockingArtifactStore) Open(ctx context.Context, ref artifact.Ref) (io.ReadCloser, error) {
	reader, err := s.Store.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &blockingArtifactReader{ReadCloser: reader, ctx: ctx, started: s.started, once: &s.once}, nil
}

type blockingArtifactReader struct {
	io.ReadCloser
	ctx     context.Context
	started chan struct{}
	once    *sync.Once
}

func (r *blockingArtifactReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}
