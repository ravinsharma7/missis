package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const idempotencyRequestHashV1Prefix = "missis-request-v1:"

type idempotencyRequestHashContextKey struct{}

// ComputeIdempotencyRequestHashV1 binds an idempotency key to one versioned
// request envelope. The caller supplies a JSON-safe structure whose field
// order and normalization are part of that operation's request contract.
func ComputeIdempotencyRequestHashV1(request any) (string, error) {
	canonical, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode idempotency request: %w", err)
	}
	framed := append([]byte("MISSIS-IDEMPOTENCY-REQUEST\x00v1\x00"), canonical...)
	sum := sha256.Sum256(framed)
	return idempotencyRequestHashV1Prefix + hex.EncodeToString(sum[:]), nil
}

// WithIdempotencyRequestHash attaches the application-computed semantic
// request hash without changing the public append method signatures.
func WithIdempotencyRequestHash(ctx context.Context, hash string) context.Context {
	return context.WithValue(ctx, idempotencyRequestHashContextKey{}, hash)
}

func idempotencyRequestHashFromContext(ctx context.Context) string {
	hash, _ := ctx.Value(idempotencyRequestHashContextKey{}).(string)
	return hash
}

func validateIdempotencyRequestHash(hash string) error {
	if !strings.HasPrefix(hash, idempotencyRequestHashV1Prefix) {
		return fmt.Errorf("unsupported idempotency request hash version")
	}
	raw := strings.TrimPrefix(hash, idempotencyRequestHashV1Prefix)
	if len(raw) != sha256.Size*2 {
		return fmt.Errorf("invalid idempotency request hash length")
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid idempotency request hash encoding")
	}
	return nil
}

func requireMatchingIdempotencyRequest(key, storedHash, proposedHash string) error {
	if proposedHash == "" {
		return nil
	}
	if err := validateIdempotencyRequestHash(proposedHash); err != nil {
		return fmt.Errorf("idempotency key %q: %w", key, err)
	}
	if storedHash != proposedHash {
		return fmt.Errorf("%w: key %q belongs to a different request", ErrIdempotencyMismatch, key)
	}
	return nil
}

func retiredIdempotencyKeyError(key string) error {
	return fmt.Errorf("%w: key %q is a format-v2 pre-fingerprint tombstone and cannot be replayed or reused; use a new key", ErrIdempotencyMismatch, key)
}
