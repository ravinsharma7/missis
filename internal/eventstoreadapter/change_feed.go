package eventstoreadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ravinsharma7/missis/internal/model"
	neutral "github.com/ravinsharma7/skunkwork/packages/eventstore"
)

const maxChangeCursorBytes = 2048

var changeCursorDigestDomainV1 = []byte("EVENTSTORE-CHANGE-CURSOR\x00v1\x00")

type changeCursorClaimsV1 struct {
	Version        string `json:"version"`
	StoreID        string `json:"store_id"`
	IntegrityEpoch string `json:"integrity_epoch"`
	Position       uint64 `json:"position"`
}

func encodeChangeCursorV1(claims changeCursorClaimsV1) (neutral.ChangeCursor, error) {
	if claims.Version != neutral.ChangeCursorVersionV1 || strings.TrimSpace(claims.StoreID) == "" || strings.TrimSpace(claims.IntegrityEpoch) == "" {
		return "", fmt.Errorf("%w: cursor claims are incomplete", neutral.ErrCursorInvalid)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("%w: encode claims: %v", neutral.ErrCursorInvalid, err)
	}
	digest := changeCursorDigestV1(payload)
	token := neutral.ChangeCursor(neutral.ChangeCursorVersionV1 + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(digest))
	if len(token) > maxChangeCursorBytes {
		return "", fmt.Errorf("%w: encoded cursor exceeds %d bytes", neutral.ErrCursorInvalid, maxChangeCursorBytes)
	}
	return token, nil
}

func decodeChangeCursorV1(cursor neutral.ChangeCursor) (changeCursorClaimsV1, error) {
	raw := string(cursor)
	if raw == "" || len(raw) > maxChangeCursorBytes {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor length is invalid", neutral.ErrCursorInvalid)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor must contain three components", neutral.ErrCursorInvalid)
	}
	if parts[0] != neutral.ChangeCursorVersionV1 {
		if strings.HasPrefix(parts[0], "eventstore-change-cursor-v") {
			return changeCursorClaimsV1{}, fmt.Errorf("%w: %q", neutral.ErrCursorVersionUnsupported, parts[0])
		}
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor prefix is invalid", neutral.ErrCursorInvalid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[1] {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor payload is not canonical base64url", neutral.ErrCursorInvalid)
	}
	providedDigest, err := hex.DecodeString(parts[2])
	if err != nil || len(providedDigest) != sha256.Size || hex.EncodeToString(providedDigest) != parts[2] {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor digest encoding is invalid", neutral.ErrCursorInvalid)
	}
	expectedDigest := changeCursorDigestV1(payload)
	if subtle.ConstantTimeCompare(providedDigest, expectedDigest) != 1 {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor digest mismatch", neutral.ErrCursorCorrupt)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var claims changeCursorClaimsV1
	if err := decoder.Decode(&claims); err != nil {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: decode claims: %v", neutral.ErrCursorInvalid, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor payload contains trailing input", neutral.ErrCursorInvalid)
	}
	canonical, err := json.Marshal(claims)
	if err != nil || !bytes.Equal(canonical, payload) {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor payload is not canonical", neutral.ErrCursorInvalid)
	}
	if claims.Version != neutral.ChangeCursorVersionV1 || strings.TrimSpace(claims.StoreID) == "" || strings.TrimSpace(claims.IntegrityEpoch) == "" {
		return changeCursorClaimsV1{}, fmt.Errorf("%w: cursor claims are incomplete", neutral.ErrCursorInvalid)
	}
	return claims, nil
}

func changeCursorDigestV1(payload []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write(changeCursorDigestDomainV1)
	_, _ = hash.Write(payload)
	return hash.Sum(nil)
}

func validateChangeCursorWindow(position, earliest, highWater uint64, retentionFloorDeclared bool) error {
	if position > highWater {
		return fmt.Errorf("%w: cursor position %d exceeds captured head %d", neutral.ErrCursorFuture, position, highWater)
	}
	if earliest > 1 && !retentionFloorDeclared {
		return fmt.Errorf("%w: earliest accepted position is %d but this adapter declares complete retention", neutral.ErrChangeFeedIntegrity, earliest)
	}
	if earliest > 0 && position < earliest-1 {
		return fmt.Errorf("%w: cursor position %d requires history before earliest retained position %d", neutral.ErrCursorStale, position, earliest)
	}
	return nil
}

func (a *Adapter) BeginChanges(ctx context.Context) (neutral.ChangeCursor, error) {
	window, err := a.store.LoadAcceptedChangeWindowContext(ctx, 0, 1)
	if err != nil {
		return "", err
	}
	position := uint64(0)
	if window.Earliest > 0 {
		position = window.Earliest - 1
	}
	return encodeChangeCursorV1(changeCursorClaimsV1{
		Version: neutral.ChangeCursorVersionV1, StoreID: window.StoreID,
		IntegrityEpoch: window.IntegrityEpoch, Position: position,
	})
}

func (a *Adapter) LatestCursor(ctx context.Context) (neutral.ChangeCursor, error) {
	window, err := a.store.LoadAcceptedChangeWindowContext(ctx, 0, 1)
	if err != nil {
		return "", err
	}
	return encodeChangeCursorV1(changeCursorClaimsV1{
		Version: neutral.ChangeCursorVersionV1, StoreID: window.StoreID,
		IntegrityEpoch: window.IntegrityEpoch, Position: window.HighWater,
	})
}

func (a *Adapter) ReadChanges(ctx context.Context, request neutral.ReadChangesRequest) (neutral.ChangePage, error) {
	if err := ctx.Err(); err != nil {
		return neutral.ChangePage{}, err
	}
	if request.Limit == 0 || request.Limit > neutral.MaxChangePageSize {
		return neutral.ChangePage{}, fmt.Errorf("%w: limit %d is outside 1..%d", neutral.ErrChangeLimitInvalid, request.Limit, neutral.MaxChangePageSize)
	}
	claims, err := decodeChangeCursorV1(request.After)
	if err != nil {
		return neutral.ChangePage{}, err
	}
	window, err := a.store.LoadAcceptedChangeWindowContext(ctx, claims.Position, request.Limit)
	if err != nil {
		return neutral.ChangePage{}, err
	}
	if claims.StoreID != window.StoreID {
		return neutral.ChangePage{}, fmt.Errorf("%w: cursor store_id=%q opened store_id=%q", neutral.ErrCursorForeignStore, claims.StoreID, window.StoreID)
	}
	if claims.IntegrityEpoch != window.IntegrityEpoch {
		return neutral.ChangePage{}, fmt.Errorf("%w: cursor epoch=%q active epoch=%q", neutral.ErrCursorEpochMismatch, claims.IntegrityEpoch, window.IntegrityEpoch)
	}
	if err := validateChangeCursorWindow(claims.Position, window.Earliest, window.HighWater, false); err != nil {
		return neutral.ChangePage{}, err
	}

	page := neutral.ChangePage{Next: request.After}
	expected := claims.Position
	for _, record := range window.Records {
		expected++
		if record.Position != expected {
			return neutral.ChangePage{}, fmt.Errorf("%w: expected accepted position %d, observed %d", neutral.ErrChangeFeedIntegrity, expected, record.Position)
		}
		if record.RecordCodec != neutral.RecordCodecV1 {
			return neutral.ChangePage{}, fmt.Errorf("%w: position=%d event_id=%q codec=%q", neutral.ErrChangeRecordUnsupported, record.Position, record.EventID, record.RecordCodec)
		}
		if computed := model.EventContentHashV1(record.AcceptedBytes); record.ContentHash != computed {
			return neutral.ChangePage{}, fmt.Errorf("%w: content digest mismatch at position=%d event_id=%q stored=%q computed=%q", neutral.ErrChangeFeedIntegrity, record.Position, record.EventID, record.ContentHash, computed)
		}
		event, err := neutral.DecodeAcceptedEventV1(record.AcceptedBytes)
		if err != nil {
			return neutral.ChangePage{}, fmt.Errorf("%w: decode position=%d event_id=%q: %v", neutral.ErrChangeFeedIntegrity, record.Position, record.EventID, err)
		}
		if event.ID != string(record.EventID) || event.Namespace != window.StoreID {
			return neutral.ChangePage{}, fmt.Errorf("%w: indexed identity differs at position=%d event_id=%q", neutral.ErrChangeFeedIntegrity, record.Position, record.EventID)
		}
		cursor, err := encodeChangeCursorV1(changeCursorClaimsV1{
			Version: neutral.ChangeCursorVersionV1, StoreID: window.StoreID,
			IntegrityEpoch: window.IntegrityEpoch, Position: record.Position,
		})
		if err != nil {
			return neutral.ChangePage{}, err
		}
		page.Changes = append(page.Changes, neutral.Change{Cursor: cursor, Event: event})
		page.Next = cursor
	}
	nextClaims := claims
	if len(page.Changes) > 0 {
		nextClaims.Position = window.Records[len(window.Records)-1].Position
	}
	page.AtHead = nextClaims.Position == window.HighWater
	return page, nil
}
