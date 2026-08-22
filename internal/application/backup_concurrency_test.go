package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/ravinsharma7/missis/internal/artifact"
	"github.com/ravinsharma7/missis/internal/store"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func TestConcurrentImportsAndBackupsProduceCoherentSnapshots(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "missis.db")
	artifactRoot := filepath.Join(dir, "artifacts")
	clock := fixedClock{fixedNow()}
	svc1, err := OpenPathWithClockAndArtifactRoot(dbPath, clock, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := OpenPathWithClockAndArtifactRoot(dbPath, clock, artifactRoot)
	if err != nil {
		_ = svc1.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc1.Close()
		_ = svc2.Close()
	})
	client1 := missis.NewClient(svc1)
	client2 := missis.NewClient(svc2)
	ctx := context.Background()
	const imports = 10
	const backups = 3
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, imports+backups)
	backupPaths := make(chan string, backups)
	for i := 0; i < imports; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client2.ImportMarkdown(ctx, missis.RequestContext{
				Actor:          "client/" + strconv.Itoa(i%2),
				IdempotencyKey: "backup-concurrent-" + strconv.Itoa(i),
			}, missis.ImportOptions{
				Content:  "# Ticket " + strconv.Itoa(i) + "\n\n## body\n\ncontent " + strconv.Itoa(i) + "\n",
				Artifact: "ticket-" + strconv.Itoa(i) + ".md",
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	for i := 0; i < backups; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			path := filepath.Join(dir, "backup-"+strconv.Itoa(i)+".db")
			if err := client1.BackupTo(ctx, path); err != nil {
				errs <- err
				return
			}
			backupPaths <- path
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(backupPaths)
	for err := range errs {
		t.Errorf("concurrent import or backup: %v", err)
	}
	var completed []string
	for path := range backupPaths {
		completed = append(completed, path)
	}
	if len(completed) == 0 {
		t.Fatal("no concurrent backup completed")
	}

	for _, backupPath := range completed {
		var manifest missis.BackupManifest
		data, err := os.ReadFile(backupPath + ".manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		backupDB, err := store.Open(backupPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := backupDB.CheckConsistency(); err != nil {
			_ = backupDB.Close()
			t.Fatalf("backup consistency: %v", err)
		}
		records, err := backupDB.ListArtifacts(ctx)
		closeErr := backupDB.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if len(records) != len(manifest.Artifacts) {
			t.Fatalf("backup %s record count=%d manifest count=%d", backupPath, len(records), len(manifest.Artifacts))
		}
		for _, record := range records {
			ref, err := artifact.ParseRef(record.Ref)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := svc1.ArtifactStore().Stat(ctx, ref)
			if err != nil {
				t.Fatalf("source artifact %s missing for completed snapshot: %v", record.Ref, err)
			}
			if metadata.Size != record.Size || metadata.Digest != record.Digest {
				t.Fatalf("source artifact %s metadata changed: %+v versus %+v", record.Ref, metadata, record)
			}
		}
	}
}
