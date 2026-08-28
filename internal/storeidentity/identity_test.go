package storeidentity

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func fixedDocument() DocumentV1 {
	var document DocumentV1
	for index := range document.Nonce {
		document.Nonce[index] = byte(index)
	}
	return document
}

func TestIdentityVectorV1(t *testing.T) {
	document := fixedDocument()
	const wantID = "store:v1:sha256:120311f35ac84b69682c5b5be1dbe7ab96994ef4a8db9d43473d8d0f1f379867"
	if got := document.StoreID(); got != wantID {
		t.Fatalf("store id = %q, want %q", got, wantID)
	}
	parsed, err := ParseCanonicalV1(document.CanonicalBytes())
	if err != nil || parsed != document {
		t.Fatalf("parse = %#v, err=%v", parsed, err)
	}
	if err := ValidateBinding(wantID, document.CanonicalBytes()); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityRejectsTamperingAndTrailingBytes(t *testing.T) {
	document := fixedDocument()
	raw := document.CanonicalBytes()
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-1] ^= 1
	if err := ValidateBinding(document.StoreID(), tampered); err == nil {
		t.Fatal("tampered identity document validated")
	}
	if _, err := ParseCanonicalV1(append(raw, 0)); err == nil {
		t.Fatal("trailing byte was accepted")
	}
}

func TestIdentityEntropyFailureDoesNotCreateDocument(t *testing.T) {
	oldEntropy := entropy
	entropy = bytes.NewReader(nil)
	t.Cleanup(func() { entropy = oldEntropy })
	if _, err := NewDocumentV1(); !errors.Is(err, bytes.ErrTooLarge) && err == nil {
		t.Fatal("entropy failure was ignored")
	}
}

func TestStoreIDGrammar(t *testing.T) {
	id := fixedDocument().StoreID()
	if err := ValidateStoreID(id); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoreID(strings.ToUpper(id)); err == nil {
		t.Fatal("uppercase store id was accepted")
	}
}

func TestJavaScriptIdentityVectorVerifier(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; JavaScript conformance verifier retained for language-capable CI")
	}
	command := exec.Command(node, "../../specs/vectors/verify-store-identity-v1.mjs")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript identity vector verifier: %v\n%s", err, output)
	}
}
