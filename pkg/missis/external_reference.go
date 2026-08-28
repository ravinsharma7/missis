package missis

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ravinsharma7/missis/internal/externalref"
	"github.com/ravinsharma7/missis/internal/storeidentity"
)

const ExternalReferenceVersionV1 = externalref.VersionV1

var ErrExternalReferenceUnsupported = errors.New("external reference resolution is not supported by this service")

// ExternalAuthorityError is a path-free, stable failure returned while
// constructing or reading an authority snapshot.
type ExternalAuthorityError struct {
	Code           string
	Retryable      bool
	OperatorAction string
}

func (e *ExternalAuthorityError) Error() string {
	if e == nil {
		return "external authority failure"
	}
	return e.Code
}

// ExternalReferenceV1 is an alpha API identifying an entity owned by another
// immutable store identity. It deliberately has no filesystem path, URI,
// hostname, or credential field; reachability is supplied by caller-owned
// peer handles.
type ExternalReferenceV1 = externalref.ReferenceV1
type ExternalReferencePinV1 = externalref.PinV1
type ExternalReferenceObservedV1 = externalref.ObservedV1

func validateExternalToken(name, value string) error {
	if value == "" {
		return fmt.Errorf("external reference %s is required", name)
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t ") {
		return fmt.Errorf("external reference %s contains whitespace or control characters", name)
	}
	return nil
}

// IdentityKey returns an unambiguous key for fields that define identity.
// Display hints and observations are deliberately excluded.
// ParseExternalReferenceV1 rejects unknown fields as well as trailing JSON.
// This keeps locator/path fields inert instead of silently accepting them.
func ParseExternalReferenceV1(raw []byte) (ExternalReferenceV1, error) {
	return externalref.ParseV1(raw)
}

type ExternalAuthorityState string

const (
	ExternalAuthorityVerified     ExternalAuthorityState = "verified"
	ExternalAuthorityDegraded     ExternalAuthorityState = "degraded"
	ExternalAuthorityUnavailable  ExternalAuthorityState = "unavailable"
	ExternalAuthorityUnauthorized ExternalAuthorityState = "unauthorized"
)

type ExternalIdentityState string

const (
	ExternalIdentityMatched      ExternalIdentityState = "matched"
	ExternalIdentityMissing      ExternalIdentityState = "missing"
	ExternalIdentityKindMismatch ExternalIdentityState = "kind-mismatch"
	ExternalIdentityCollision    ExternalIdentityState = "identity-collision"
	ExternalIdentityUnsupported  ExternalIdentityState = "unsupported"
	ExternalIdentityUnknown      ExternalIdentityState = "unknown"
)

type ExternalLifecycle string

const (
	ExternalLifecycleActive          ExternalLifecycle = "active"
	ExternalLifecycleRetracted       ExternalLifecycle = "retracted"
	ExternalLifecycleNotYetEffective ExternalLifecycle = "not-yet-effective"
	ExternalLifecycleSuperseded      ExternalLifecycle = "superseded"
	ExternalLifecycleUnknown         ExternalLifecycle = "unknown"
)

type ExternalFreshness string

const (
	ExternalFreshnessCurrent    ExternalFreshness = "current"
	ExternalFreshnessStale      ExternalFreshness = "stale"
	ExternalFreshnessUnverified ExternalFreshness = "unverified"
)

type ExternalResolutionQuery struct {
	EffectiveAt time.Time `json:"effective_at,omitempty"`
	KnownAt     time.Time `json:"known_at,omitempty"`
}

type ExternalResolutionV1 struct {
	Reference                ExternalReferenceV1     `json:"reference"`
	AuthorityState           ExternalAuthorityState  `json:"authority_state"`
	IdentityState            ExternalIdentityState   `json:"identity_state"`
	Lifecycle                ExternalLifecycle       `json:"lifecycle"`
	Freshness                ExternalFreshness       `json:"freshness"`
	StreamRevision           uint64                  `json:"stream_revision,omitempty"`
	CurrentEventID           string                  `json:"current_event_id,omitempty"`
	VerifiedCheckpointDigest string                  `json:"verified_checkpoint_digest,omitempty"`
	VerifiedThroughCursor    string                  `json:"verified_through_cursor,omitempty"`
	ProjectionID             string                  `json:"projection_id,omitempty"`
	ProjectionVersion        string                  `json:"projection_version,omitempty"`
	KnownAt                  time.Time               `json:"known_at,omitempty"`
	EffectiveAt              time.Time               `json:"effective_at,omitempty"`
	EvidenceRefs             []string                `json:"evidence_refs,omitempty"`
	PeerInsights             []ExternalPeerInsightV1 `json:"peer_insights,omitempty"`
	Failure                  *ExternalFailureV1      `json:"failure,omitempty"`
	Warnings                 []string                `json:"warnings,omitempty"`
}

type ExternalFailurePolicy string

const (
	ExternalPolicyFailFast   ExternalFailurePolicy = "fail-fast"
	ExternalPolicyFailClosed ExternalFailurePolicy = "fail-closed"
	ExternalPolicyFailSafe   ExternalFailurePolicy = "fail-safe"
	ExternalPolicyFailStop   ExternalFailurePolicy = "fail-stop"
	ExternalPolicyFailOver   ExternalFailurePolicy = "fail-over"
)

type ExternalFieldDifferenceV1 struct {
	Field    string `json:"field"`
	Expected string `json:"expected,omitempty"`
	Claimed  string `json:"claimed,omitempty"`
}

type ExternalFailureV1 struct {
	Code           string                      `json:"code"`
	Policy         ExternalFailurePolicy       `json:"policy"`
	Retryable      bool                        `json:"retryable"`
	OperatorAction string                      `json:"operator_action,omitempty"`
	Differences    []ExternalFieldDifferenceV1 `json:"differences,omitempty"`
}

const StoreIdentityClaimVersionV1 = "store-identity-claim-v1"

// StoreIdentityClaimV1 describes the identity and current observable state of
// one reachable peer. IdentityDigest binds the immutable identity fields;
// GenesisDigest, HeadDigest, and EventCount make same-ID clashes and divergent
// replicas diagnosable without exposing a filesystem location.
type StoreIdentityClaimV1 struct {
	Version              string `json:"version"`
	StoreID              string `json:"store_id"`
	IdentityScheme       string `json:"identity_scheme"`
	IdentityDocument     []byte `json:"identity_document,omitempty"`
	IdentityDigest       string `json:"identity_digest"`
	GenesisDigest        string `json:"genesis_digest,omitempty"`
	GenesisDigestScheme  string `json:"genesis_digest_scheme,omitempty"`
	HeadDigest           string `json:"head_digest,omitempty"`
	HeadIntegrityEpoch   string `json:"head_integrity_epoch,omitempty"`
	EventCount           int64  `json:"event_count"`
	FormatRevision       int    `json:"format_revision"`
	ProtocolVersion      string `json:"protocol_version,omitempty"`
	ContractBundleDigest string `json:"contract_bundle_digest,omitempty"`
	LineageReceiptDigest string `json:"lineage_receipt_digest,omitempty"`
}

func NewHashedStoreIdentityClaimV1(storeID string, document []byte, genesisDigest, genesisEpoch, headDigest, headEpoch string, eventCount int64, formatRevision int) StoreIdentityClaimV1 {
	return StoreIdentityClaimV1{
		Version: StoreIdentityClaimVersionV1, StoreID: storeID, IdentityScheme: storeidentity.Scheme,
		IdentityDocument: append([]byte(nil), document...), IdentityDigest: storeidentity.DocumentDigest(document),
		GenesisDigest: genesisDigest, GenesisDigestScheme: genesisEpoch,
		HeadDigest: headDigest, HeadIntegrityEpoch: headEpoch,
		EventCount: eventCount, FormatRevision: formatRevision, ProtocolVersion: "eventstore-v3-alpha.4",
	}
}

// NewStoreIdentityClaimV1 constructs a claim with the required
// domain-separated identity digest. The current Missis adapter reports its
// pre-v3 IDs as missis-ulid-v1; v3 self-certifying IDs use eventstore-hash-v1.
func NewStoreIdentityClaimV1(storeID, identityScheme, genesisDigest, headDigest string, eventCount int64, formatRevision int) StoreIdentityClaimV1 {
	return StoreIdentityClaimV1{
		Version:        StoreIdentityClaimVersionV1,
		StoreID:        storeID,
		IdentityScheme: identityScheme,
		IdentityDigest: storeIdentityClaimDigestV1(storeID, identityScheme, genesisDigest),
		GenesisDigest:  genesisDigest,
		HeadDigest:     headDigest,
		EventCount:     eventCount,
		FormatRevision: formatRevision,
	}
}

func (c StoreIdentityClaimV1) Validate() error {
	if c.Version != StoreIdentityClaimVersionV1 {
		return fmt.Errorf("unsupported store identity claim version %q", c.Version)
	}
	if err := validateExternalToken("claim.store_id", c.StoreID); err != nil {
		return err
	}
	if !strings.HasPrefix(c.StoreID, "store:") {
		return fmt.Errorf("store identity claim store_id must use the store: prefix")
	}
	if err := validateExternalToken("claim.identity_scheme", c.IdentityScheme); err != nil {
		return err
	}
	if c.EventCount < 0 {
		return fmt.Errorf("store identity claim event_count must not be negative")
	}
	if c.FormatRevision < 1 {
		return fmt.Errorf("store identity claim format_revision must be positive")
	}
	if c.IdentityScheme == storeidentity.Scheme {
		if err := storeidentity.ValidateBinding(c.StoreID, c.IdentityDocument); err != nil {
			return fmt.Errorf("store identity claim document: %w", err)
		}
		if c.IdentityDigest != storeidentity.DocumentDigest(c.IdentityDocument) {
			return fmt.Errorf("store identity claim digest does not match its document")
		}
		if c.GenesisDigestScheme == "" || c.HeadIntegrityEpoch == "" || c.ProtocolVersion == "" {
			return fmt.Errorf("store identity claim integrity/protocol metadata is incomplete")
		}
		if c.LineageReceiptDigest != "" && !strings.HasPrefix(c.LineageReceiptDigest, "sha256:") {
			return fmt.Errorf("store identity claim lineage receipt digest is invalid")
		}
	} else {
		expected := storeIdentityClaimDigestV1(c.StoreID, c.IdentityScheme, c.GenesisDigest)
		if c.IdentityDigest != expected {
			return fmt.Errorf("store identity claim digest does not match its immutable fields")
		}
	}
	return nil
}

func storeIdentityClaimDigestV1(storeID, identityScheme, genesisDigest string) string {
	// Length framing prevents concatenation ambiguity and keeps this digest
	// independent from event-chain hashing.
	fields := []string{"MISSIS-STORE-IDENTITY-CLAIM-V1", identityScheme, storeID, genesisDigest}
	var framed strings.Builder
	for _, field := range fields {
		framed.WriteString(strconv.Itoa(len(field)))
		framed.WriteByte(':')
		framed.WriteString(field)
	}
	sum := sha256.Sum256([]byte(framed.String()))
	return fmt.Sprintf("sha256:%x", sum[:])
}

type ExternalPeerClassification string

const (
	ExternalPeerMatching          ExternalPeerClassification = "matching-peer"
	ExternalPeerDifferentStore    ExternalPeerClassification = "different-store"
	ExternalPeerUnreachable       ExternalPeerClassification = "unreachable"
	ExternalPeerInvalidClaim      ExternalPeerClassification = "invalid-claim"
	ExternalPeerIdentityCollision ExternalPeerClassification = "identity-collision"
	ExternalPeerExactReplica      ExternalPeerClassification = "exact-replica"
	ExternalPeerDivergentState    ExternalPeerClassification = "divergent-state-unverified"
)

// ExternalPeerInsightV1 is deliberately diagnostic. It states what a peer
// claimed and which fields differ, without leaking its path, URI, credentials,
// or raw transport error.
type ExternalPeerInsightV1 struct {
	PeerIndex           int                         `json:"peer_index"`
	Classification      ExternalPeerClassification  `json:"classification"`
	ExpectedStoreID     string                      `json:"expected_store_id"`
	ConfiguredStoreID   string                      `json:"configured_store_id,omitempty"`
	ClaimedStoreID      string                      `json:"claimed_store_id,omitempty"`
	IdentityScheme      string                      `json:"identity_scheme,omitempty"`
	IdentityDigest      string                      `json:"identity_digest,omitempty"`
	GenesisDigest       string                      `json:"genesis_digest,omitempty"`
	GenesisDigestScheme string                      `json:"genesis_digest_scheme,omitempty"`
	HeadDigest          string                      `json:"head_digest,omitempty"`
	HeadIntegrityEpoch  string                      `json:"head_integrity_epoch,omitempty"`
	EventCount          int64                       `json:"event_count,omitempty"`
	FormatRevision      int                         `json:"format_revision,omitempty"`
	ProtocolVersion     string                      `json:"protocol_version,omitempty"`
	FieldDifferences    []ExternalFieldDifferenceV1 `json:"field_differences,omitempty"`
	Differences         []string                    `json:"differences,omitempty"`
}

// ExternalAuthority is an already-authorized opaque peer handle. Opening a
// snapshot binds its identity claim and entity resolution to one authority
// state, preventing a writer from changing the state between those steps.
type ExternalAuthority interface {
	OpenExternalResolutionSnapshot(context.Context) (ExternalAuthoritySnapshot, error)
}

// ExternalAuthorityExpectation is implemented by operator-bound peers. The
// configured ID constrains authority; it is not accepted as proof of identity.
type ExternalAuthorityExpectation interface {
	ExpectedExternalStoreID() string
}

type ExternalAuthoritySnapshot interface {
	StoreIdentityClaimContext(context.Context) (StoreIdentityClaimV1, error)
	ResolveExternalReferenceContext(context.Context, ExternalReferenceV1, ExternalResolutionQuery) (ExternalResolutionV1, error)
	Close() error
}

// PeerResolver queries the peer handles available to this process. It is not
// a registry and has no central registration state: peers prove what store
// they expose each time they are queried.
type PeerResolver struct {
	mu    sync.RWMutex
	peers []ExternalAuthority
}

type externalPeerCandidate struct {
	snapshot ExternalAuthoritySnapshot
	claim    StoreIdentityClaimV1
	index    int
}

func NewPeerResolver(peers ...ExternalAuthority) *PeerResolver {
	r := &PeerResolver{}
	for _, peer := range peers {
		if peer != nil {
			r.peers = append(r.peers, peer)
		}
	}
	return r
}

// AddPeer makes an already-authorized peer reachable to this resolver. It does
// not associate the peer with a claimed store ID; that claim is queried and
// verified during every resolution.
func (r *PeerResolver) AddPeer(peer ExternalAuthority) error {
	if peer == nil {
		return fmt.Errorf("peer authority is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers = append(r.peers, peer)
	return nil
}

func (r *PeerResolver) Resolve(ctx context.Context, ref ExternalReferenceV1, query ExternalResolutionQuery) (ExternalResolutionV1, error) {
	if err := ctx.Err(); err != nil {
		return ExternalResolutionV1{}, err
	}
	if err := ref.Validate(); err != nil {
		return ExternalResolutionV1{}, err
	}
	base := ExternalResolutionV1{
		Reference:      ref,
		AuthorityState: ExternalAuthorityUnavailable,
		IdentityState:  ExternalIdentityUnknown,
		Lifecycle:      ExternalLifecycleUnknown,
		Freshness:      ExternalFreshnessUnverified,
	}
	r.mu.RLock()
	peers := append([]ExternalAuthority(nil), r.peers...)
	r.mu.RUnlock()
	var matches []externalPeerCandidate
	var opened []ExternalAuthoritySnapshot
	var firstAccessFailure *ExternalAuthorityError
	defer func() {
		for _, snapshot := range opened {
			_ = snapshot.Close()
		}
	}()
	for index, peer := range peers {
		insight := ExternalPeerInsightV1{PeerIndex: index, ExpectedStoreID: ref.StoreID}
		configuredStoreID := ""
		if expected, ok := peer.(ExternalAuthorityExpectation); ok {
			configuredStoreID = expected.ExpectedExternalStoreID()
			insight.ConfiguredStoreID = configuredStoreID
		}
		snapshot, err := peer.OpenExternalResolutionSnapshot(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ExternalResolutionV1{}, ctxErr
			}
			insight.Classification = ExternalPeerUnreachable
			var accessFailure *ExternalAuthorityError
			if errors.As(err, &accessFailure) {
				insight.Differences = []string{accessFailure.Code}
				if firstAccessFailure == nil {
					firstAccessFailure = accessFailure
				}
			} else {
				insight.Differences = []string{"identity claim unavailable"}
			}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		if snapshot == nil {
			insight.Classification = ExternalPeerInvalidClaim
			insight.Differences = []string{"authority returned a nil snapshot"}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		opened = append(opened, snapshot)
		claim, err := snapshot.StoreIdentityClaimContext(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ExternalResolutionV1{}, ctxErr
			}
			insight.Classification = ExternalPeerUnreachable
			insight.Differences = []string{"identity claim unavailable"}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		insight.ClaimedStoreID = claim.StoreID
		insight.IdentityScheme = claim.IdentityScheme
		insight.IdentityDigest = claim.IdentityDigest
		insight.GenesisDigest = claim.GenesisDigest
		insight.GenesisDigestScheme = claim.GenesisDigestScheme
		insight.HeadDigest = claim.HeadDigest
		insight.HeadIntegrityEpoch = claim.HeadIntegrityEpoch
		insight.EventCount = claim.EventCount
		insight.FormatRevision = claim.FormatRevision
		insight.ProtocolVersion = claim.ProtocolVersion
		if err := claim.Validate(); err != nil {
			insight.Classification = ExternalPeerInvalidClaim
			insight.Differences = []string{err.Error()}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		if configuredStoreID != "" && claim.StoreID != configuredStoreID {
			insight.Classification = ExternalPeerDifferentStore
			insight.Differences = []string{fmt.Sprintf("configured store_id expected %q but peer claimed %q", configuredStoreID, claim.StoreID)}
			insight.FieldDifferences = []ExternalFieldDifferenceV1{{Field: "configured_store_id", Expected: configuredStoreID, Claimed: claim.StoreID}}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		if claim.StoreID != ref.StoreID {
			insight.Classification = ExternalPeerDifferentStore
			insight.Differences = []string{fmt.Sprintf("store_id expected %q but peer claimed %q", ref.StoreID, claim.StoreID)}
			insight.FieldDifferences = []ExternalFieldDifferenceV1{{Field: "store_id", Expected: ref.StoreID, Claimed: claim.StoreID}}
			base.PeerInsights = append(base.PeerInsights, insight)
			continue
		}
		insight.Classification = ExternalPeerMatching
		base.PeerInsights = append(base.PeerInsights, insight)
		matches = append(matches, externalPeerCandidate{snapshot: snapshot, claim: claim, index: index})
	}
	if len(matches) == 0 {
		base.Warnings = []string{"no reachable peer claimed the requested store identity"}
		if firstAccessFailure != nil {
			base.Failure = &ExternalFailureV1{Code: firstAccessFailure.Code, Policy: ExternalPolicyFailSafe, Retryable: firstAccessFailure.Retryable, OperatorAction: firstAccessFailure.OperatorAction}
		} else {
			base.Failure = &ExternalFailureV1{Code: "no-matching-peer", Policy: ExternalPolicyFailSafe, Retryable: true, OperatorAction: "supply or recover an authorized peer and re-resolve the unchanged reference"}
		}
		return base, nil
	}
	first := matches[0]
	for _, match := range matches[1:] {
		if match.claim.IdentityDigest != first.claim.IdentityDigest || match.claim.GenesisDigest != first.claim.GenesisDigest {
			base.AuthorityState = ExternalAuthorityDegraded
			base.IdentityState = ExternalIdentityCollision
			base.Warnings = []string{"peers claimed the same store_id with different immutable identity evidence"}
			base.Failure = &ExternalFailureV1{Code: "identity-collision", Policy: ExternalPolicyFailClosed, Retryable: false, OperatorAction: "quarantine candidates and inspect migration or clone provenance"}
			markPeerConflict(base.PeerInsights, peerCandidateIndexes(matches), ExternalPeerIdentityCollision, []string{"identity_digest or genesis_digest differs between same-ID peers"})
			return base, nil
		}
	}
	for _, match := range matches[1:] {
		if match.claim.HeadDigest != first.claim.HeadDigest || match.claim.EventCount != first.claim.EventCount {
			base.AuthorityState = ExternalAuthorityDegraded
			base.IdentityState = ExternalIdentityMatched
			base.Warnings = []string{"same-identity peers report different ledger state; ancestry is unverified"}
			base.Failure = &ExternalFailureV1{Code: "divergent-state-unverified", Policy: ExternalPolicyFailClosed, Retryable: false, OperatorAction: "obtain ancestry or checkpoint proof, or declare a writable fork"}
			markPeerConflict(base.PeerInsights, peerCandidateIndexes(matches), ExternalPeerDivergentState, []string{"head_digest or event_count differs; no peer was selected"})
			return base, nil
		}
	}
	if len(matches) > 1 {
		markPeerConflict(base.PeerInsights, peerCandidateIndexes(matches), ExternalPeerExactReplica, nil)
	}
	resolved, err := first.snapshot.ResolveExternalReferenceContext(ctx, ref, query)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ExternalResolutionV1{}, ctxErr
		}
		base.AuthorityState = ExternalAuthorityDegraded
		base.Warnings = []string{"store authority could not resolve the reference"}
		base.Failure = &ExternalFailureV1{Code: "authority-resolution-failed", Policy: ExternalPolicyFailSafe, Retryable: true}
		return base, nil
	}
	resolved.Reference = ref
	resolved.PeerInsights = base.PeerInsights
	if resolved.AuthorityState == "" {
		resolved.AuthorityState = ExternalAuthorityVerified
	}
	if resolved.IdentityState == "" {
		resolved.IdentityState = ExternalIdentityUnknown
	}
	if resolved.Lifecycle == "" {
		resolved.Lifecycle = ExternalLifecycleUnknown
	}
	if resolved.Freshness == "" {
		resolved.Freshness = ExternalFreshnessUnverified
	}
	if resolved.AuthorityState == ExternalAuthorityVerified && resolved.IdentityState == ExternalIdentityMatched {
		resolved.Freshness = freshnessAgainstObservation(ref.Observation, resolved)
	}
	return resolved, nil
}

func peerCandidateIndexes(matches []externalPeerCandidate) []int {
	indexes := make([]int, 0, len(matches))
	for _, match := range matches {
		indexes = append(indexes, match.index)
	}
	return indexes
}

func markPeerConflict(insights []ExternalPeerInsightV1, indexes []int, classification ExternalPeerClassification, differences []string) {
	matchIndexes := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		matchIndexes[index] = struct{}{}
	}
	for index := range insights {
		if _, ok := matchIndexes[insights[index].PeerIndex]; ok {
			insights[index].Classification = classification
			insights[index].Differences = append([]string(nil), differences...)
		}
	}
}

func freshnessAgainstObservation(observed *ExternalReferenceObservedV1, resolved ExternalResolutionV1) ExternalFreshness {
	if observed == nil {
		return ExternalFreshnessCurrent
	}
	if observed.StreamRevision != nil && *observed.StreamRevision != resolved.StreamRevision {
		return ExternalFreshnessStale
	}
	if observed.CurrentEventID != "" && observed.CurrentEventID != resolved.CurrentEventID {
		return ExternalFreshnessStale
	}
	return ExternalFreshnessCurrent
}
