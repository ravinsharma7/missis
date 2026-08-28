// Package storeidentity implements the location-independent store identity
// codec. It has no dependency on SQLite or Missis domain types.
package storeidentity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	DocumentVersion = "store-identity-document-v1"
	Scheme          = "eventstore-hash-v1"
	StoreIDPrefix   = "store:v1:sha256:"
	nonceSize       = 32
)

var (
	documentDomain           = []byte("MISSIS-EVENTSTORE-IDENTITY-DOCUMENT\x00v1\x00")
	storeIDDomain            = []byte("MISSIS-EVENTSTORE-IDENTITY\x00v1\x00")
	entropy        io.Reader = rand.Reader
)

type DocumentV1 struct {
	Nonce [nonceSize]byte
}

func NewDocumentV1() (DocumentV1, error) {
	var document DocumentV1
	if _, err := io.ReadFull(entropy, document.Nonce[:]); err != nil {
		return DocumentV1{}, fmt.Errorf("generate store identity nonce: %w", err)
	}
	return document, nil
}

// CanonicalBytes uses fixed-order, unsigned-big-endian length framing. A new
// field or field order requires a new document version.
func (d DocumentV1) CanonicalBytes() []byte {
	result := append([]byte(nil), documentDomain...)
	result = appendField(result, []byte(DocumentVersion))
	result = appendField(result, []byte(Scheme))
	result = appendField(result, d.Nonce[:])
	return result
}

func ParseCanonicalV1(raw []byte) (DocumentV1, error) {
	if !strings.HasPrefix(string(raw), string(documentDomain)) {
		return DocumentV1{}, errors.New("store identity document has unknown domain or version")
	}
	rest := raw[len(documentDomain):]
	version, rest, err := consumeField(rest)
	if err != nil || string(version) != DocumentVersion {
		return DocumentV1{}, errors.New("store identity document has invalid version")
	}
	scheme, rest, err := consumeField(rest)
	if err != nil || string(scheme) != Scheme {
		return DocumentV1{}, errors.New("store identity document has invalid scheme")
	}
	nonce, rest, err := consumeField(rest)
	if err != nil || len(nonce) != nonceSize || len(rest) != 0 {
		return DocumentV1{}, errors.New("store identity document has invalid nonce or trailing bytes")
	}
	var document DocumentV1
	copy(document.Nonce[:], nonce)
	if string(document.CanonicalBytes()) != string(raw) {
		return DocumentV1{}, errors.New("store identity document is not canonical")
	}
	return document, nil
}

func (d DocumentV1) StoreID() string {
	sum := sha256.Sum256(append(append([]byte(nil), storeIDDomain...), d.CanonicalBytes()...))
	return StoreIDPrefix + hex.EncodeToString(sum[:])
}

func DocumentDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ValidateBinding(storeID string, raw []byte) error {
	document, err := ParseCanonicalV1(raw)
	if err != nil {
		return err
	}
	if storeID != document.StoreID() {
		return fmt.Errorf("store identity document computes %q, stored id is %q", document.StoreID(), storeID)
	}
	return nil
}

func ValidateStoreID(storeID string) error {
	if !strings.HasPrefix(storeID, StoreIDPrefix) {
		return fmt.Errorf("store_id must start with %q", StoreIDPrefix)
	}
	hexPart := strings.TrimPrefix(storeID, StoreIDPrefix)
	if len(hexPart) != sha256.Size*2 || strings.ToLower(hexPart) != hexPart {
		return errors.New("store_id digest must be 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(hexPart)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("store_id digest must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func appendField(dst, field []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(field)))
	dst = append(dst, length[:]...)
	return append(dst, field...)
}

func consumeField(raw []byte) ([]byte, []byte, error) {
	if len(raw) < 4 {
		return nil, nil, io.ErrUnexpectedEOF
	}
	length := int(binary.BigEndian.Uint32(raw[:4]))
	if length < 0 || len(raw)-4 < length {
		return nil, nil, io.ErrUnexpectedEOF
	}
	return raw[4 : 4+length], raw[4+length:], nil
}
