package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	globalJSONIntegrityEpochV1     = "global-json-chain-v1"
	canonicalEventIntegrityEpochV1 = "canonical-event-chain-v1"
	canonicalEventRecordCodecV1    = "missis-event-canonical-json-v1"
	neutralEventRecordCodecV1      = "eventstore-record-json-v1"
)

type integrityEpochTransitionReceiptV1 struct {
	Version                 string `json:"version"`
	StoreID                 string `json:"store_id"`
	SourceIntegrityEpoch    string `json:"source_integrity_epoch"`
	SourceHeadDigest        string `json:"source_head_digest"`
	SourceEventCount        int64  `json:"source_event_count"`
	ActivationAfterAliasSeq uint64 `json:"activation_after_alias_seq"`
	TargetIntegrityEpoch    string `json:"target_integrity_epoch"`
	RecordCodec             string `json:"record_codec"`
	FirstEventID            string `json:"first_event_id"`
	FirstContentHash        string `json:"first_content_hash"`
	FirstHeadDigest         string `json:"first_head_digest"`
	FormatRevision          int    `json:"format_revision"`
	ActivatedAt             string `json:"activated_at"`
}

type integrityEpochObservation struct {
	SourceHead              string
	SourceEventCount        int64
	ActivationAfterAliasSeq uint64
	FirstEventID            string
	RecordCodec             string
	FirstContentHash        string
	FirstHead               string
}

func activateCanonicalEventEpochTx(
	ctx context.Context,
	tx *sql.Tx,
	sourceHead string,
	sourceEventCount int64,
	activationAfterAliasSeq uint64,
	firstEventID, recordCodec, firstContentHash, firstHead string,
	activatedAt time.Time,
) error {
	var storeID, activeEpoch string
	if err := tx.QueryRowContext(ctx, `SELECT store_id,integrity_epoch FROM store_meta WHERE singleton=1`).Scan(&storeID, &activeEpoch); err != nil {
		return err
	}
	if activeEpoch != globalJSONIntegrityEpochV1 {
		return fmt.Errorf("integrity epoch transition requires %q, found %q", globalJSONIntegrityEpochV1, activeEpoch)
	}
	receipt := integrityEpochTransitionReceiptV1{
		Version:                 "integrity-epoch-transition-v1",
		StoreID:                 storeID,
		SourceIntegrityEpoch:    globalJSONIntegrityEpochV1,
		SourceHeadDigest:        sourceHead,
		SourceEventCount:        sourceEventCount,
		ActivationAfterAliasSeq: activationAfterAliasSeq,
		TargetIntegrityEpoch:    canonicalEventIntegrityEpochV1,
		RecordCodec:             recordCodec,
		FirstEventID:            firstEventID,
		FirstContentHash:        firstContentHash,
		FirstHeadDigest:         firstHead,
		FormatRevision:          CurrentStoreFormatRevision,
		ActivatedAt:             activatedAt.UTC().Format(time.RFC3339Nano),
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(receiptBytes)
	hexSum := hex.EncodeToString(sum[:])
	receiptID := "integrity-epoch-transition:" + hexSum
	receiptDigest := "sha256:" + hexSum
	if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_epoch_transition_receipts(
		receipt_id,store_id,source_integrity_epoch,source_head_digest,source_event_count,
		activation_after_alias_seq,target_integrity_epoch,record_codec,first_event_id,
		first_content_hash,first_head_digest,format_revision,activated_at,receipt_bytes,receipt_digest
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		receiptID, storeID, receipt.SourceIntegrityEpoch, sourceHead, sourceEventCount,
		activationAfterAliasSeq, receipt.TargetIntegrityEpoch, receipt.RecordCodec, firstEventID,
		firstContentHash, firstHead, receipt.FormatRevision, receipt.ActivatedAt, receiptBytes, receiptDigest,
	); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE store_meta SET integrity_epoch=? WHERE singleton=1`, canonicalEventIntegrityEpochV1)
	return err
}

func verifyIntegrityEpochTransitionReceipt(ctx context.Context, db contextSQL, observed *integrityEpochObservation) error {
	rows, err := db.QueryContext(ctx, `SELECT
		receipt_id,store_id,source_integrity_epoch,source_head_digest,source_event_count,
		activation_after_alias_seq,target_integrity_epoch,record_codec,first_event_id,
		first_content_hash,first_head_digest,format_revision,activated_at,receipt_bytes,receipt_digest
		FROM integrity_epoch_transition_receipts`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var receiptID, storeID, sourceEpoch, sourceHead, targetEpoch, codec string
		var firstEventID, firstContentHash, firstHead, activatedAt, receiptDigest string
		var sourceEventCount int64
		var activationAfterAliasSeq uint64
		var formatRevision int
		var receiptBytes []byte
		if err := rows.Scan(&receiptID, &storeID, &sourceEpoch, &sourceHead, &sourceEventCount,
			&activationAfterAliasSeq, &targetEpoch, &codec, &firstEventID,
			&firstContentHash, &firstHead, &formatRevision, &activatedAt, &receiptBytes, &receiptDigest); err != nil {
			return err
		}
		sum := sha256.Sum256(receiptBytes)
		hexSum := hex.EncodeToString(sum[:])
		if receiptID != "integrity-epoch-transition:"+hexSum || receiptDigest != "sha256:"+hexSum {
			return fmt.Errorf("integrity epoch transition receipt %q digest mismatch", receiptID)
		}
		var receipt integrityEpochTransitionReceiptV1
		if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
			return fmt.Errorf("integrity epoch transition receipt %q cannot be decoded: %w", receiptID, err)
		}
		if receipt.Version != "integrity-epoch-transition-v1" || receipt.StoreID != storeID ||
			receipt.SourceIntegrityEpoch != sourceEpoch || receipt.SourceHeadDigest != sourceHead ||
			receipt.SourceEventCount != sourceEventCount || receipt.ActivationAfterAliasSeq != activationAfterAliasSeq ||
			receipt.TargetIntegrityEpoch != targetEpoch || receipt.RecordCodec != codec ||
			receipt.FirstEventID != firstEventID || receipt.FirstContentHash != firstContentHash ||
			receipt.FirstHeadDigest != firstHead || receipt.FormatRevision != formatRevision || receipt.ActivatedAt != activatedAt {
			return fmt.Errorf("integrity epoch transition receipt %q indexed fields disagree with receipt bytes", receiptID)
		}
		if observed == nil || sourceEpoch != globalJSONIntegrityEpochV1 || targetEpoch != canonicalEventIntegrityEpochV1 ||
			(codec != canonicalEventRecordCodecV1 && codec != neutralEventRecordCodecV1) || codec != observed.RecordCodec || formatRevision != CurrentStoreFormatRevision ||
			sourceHead != observed.SourceHead || sourceEventCount != observed.SourceEventCount ||
			activationAfterAliasSeq != observed.ActivationAfterAliasSeq || firstEventID != observed.FirstEventID ||
			firstContentHash != observed.FirstContentHash || firstHead != observed.FirstHead {
			return fmt.Errorf("integrity epoch transition receipt %q does not match the observed chain boundary", receiptID)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if observed == nil && count != 0 {
		return fmt.Errorf("integrity epoch transition receipt exists without an observed transition")
	}
	if observed != nil && count == 0 && observed.SourceEventCount == 0 && observed.SourceHead == "" && observed.ActivationAfterAliasSeq == 0 {
		var migrationCount int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_format_migration_receipts WHERE target_format_revision=?`, CurrentStoreFormatRevision).Scan(&migrationCount); err != nil {
			return err
		}
		if migrationCount == 0 {
			return nil // canonical genesis in a store created directly at this format
		}
	}
	if observed != nil && count != 1 {
		return fmt.Errorf("observed integrity epoch transition has %d receipts, want 1", count)
	}
	return nil
}
