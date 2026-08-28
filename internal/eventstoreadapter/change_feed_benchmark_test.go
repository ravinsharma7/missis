package eventstoreadapter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

const changeFeedBenchmarkRecords = 100_000

// BenchmarkChangeFeedPage measures only bounded accepted-record traversal.
// Fixture insertion bypasses append/projection work, which remains owned by
// ticket #119 and must not be conflated with this read-path benchmark.
func BenchmarkChangeFeedPage(b *testing.B) {
	ctx := context.Background()
	path := filepath.Join(b.TempDir(), "feed-benchmark.db")
	ledger, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ledger.Close() })
	begin, err := ledger.BeginChanges(ctx)
	if err != nil {
		b.Fatal(err)
	}
	storeID, err := ledger.StoreID(ctx)
	if err != nil {
		b.Fatal(err)
	}
	seedAcceptedFeedRows(b, path, storeID, changeFeedBenchmarkRecords)

	for _, limit := range []uint32{1, 100, 1000} {
		b.Run(fmt.Sprintf("limit-%d", limit), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(limit * 256))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page, err := ledger.ReadChanges(ctx, neutral.ReadChangesRequest{After: begin, Limit: limit})
				if err != nil {
					b.Fatal(err)
				}
				if len(page.Changes) != int(limit) || page.AtHead {
					b.Fatalf("page size/head = %d/%t", len(page.Changes), page.AtHead)
				}
			}
		})
	}
}

func seedAcceptedFeedRows(b *testing.B, path, storeID string, count int) {
	b.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO events(id,stream_kind,stream_entity,sequence,event_json,record_codec,accepted_bytes,content_hash) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	at := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	stream := neutral.Ref{Kind: "benchmark", ID: "feed:benchmark"}
	for index := 1; index <= count; index++ {
		event := neutral.Event{
			ProtocolVersion: neutral.ProtocolVersionV3Alpha4,
			Namespace:       storeID,
			ID:              fmt.Sprintf("event:feed:benchmark:%06d", index),
			Stream:          stream,
			StreamRevision:  uint64(index),
			Type:            "benchmark.recorded",
			SchemaVersion:   1,
			Subject:         stream,
			PayloadCodec:    neutral.DefaultPayloadCodec,
			Payload:         []byte(`{"value":1}`),
			RecordedAt:      at,
			EffectiveAt:     at,
			Actor:           neutral.Actor{Kind: "benchmark", ID: "change-feed"},
			RecordCodec:     neutral.RecordCodecV1,
		}
		accepted, err := neutral.CanonicalAcceptedEventBytesV1(event)
		if err != nil {
			b.Fatal(err)
		}
		contentHash := sha256.Sum256(accepted)
		if _, err := statement.Exec(event.ID, stream.Kind, stream.ID, index, `{}`, neutral.RecordCodecV1, accepted, fmt.Sprintf("sha256:%x", contentHash)); err != nil {
			b.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}
