package migration

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// EventSink is the append side of the judgement hash chain, injected so this
// package never hard-couples to internal/audit. It mirrors the small sink the
// resource and isolation packages already use.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// --- Path 1: hardware fingerprint binding ----------------------------------

// HardwareBinder binds capability credentials to a simulated hardware
// fingerprint and re-validates them against the fingerprint of the machine a
// copy lands on. Simulated: not real hardware binding / root of trust.
type HardwareBinder interface {
	// Bind attaches credID to the single fingerprint it may verify on.
	// Binding the same credential twice is idempotent.
	Bind(credID string, agentID domain.AgentID, fingerprint string) error
	// Validate runs the REAL credential validation order first (revocation →
	// local state → validity window, via domain.Credential.IsUsable) and then,
	// as an additional check, requires presentFingerprint to equal the bound
	// fingerprint. An unbound credential verifies anywhere. The returned
	// reason explains a rejection (auditable).
	Validate(cred domain.Credential, now time.Time, presentFingerprint string, rc domain.RevocationChecker) (ok bool, reason string)
	// BoundFingerprint reports the fingerprint a credential is bound to ("" and
	// false when it is not bound).
	BoundFingerprint(credID string) (fingerprint string, bound bool)
}

// --- Path 2: portable work-record anchor key -------------------------------

// WorkRecord is the agent's accumulated portable history: lessons, results and
// the reputation they unlocked. It travels with the agent-owned anchor key.
type WorkRecord struct {
	AgentID    domain.AgentID `json:"agentId"`
	AnchorKey  string         `json:"anchorKey"` // simulated key id owning this record
	Reputation int            `json:"reputation"`
	Lessons    []string       `json:"lessons,omitempty"`
}

// RecordKeeper issues and revokes the simulated anchor key that makes a work
// record portable. Simulated: not a real key ecosystem.
type RecordKeeper interface {
	//Anchor issues a fresh simulated anchor key for the agent.
	Anchor(agentID domain.AgentID) string
	// RevokeAnchor voids an anchor key (L5). Revoking an unknown key is a
	// harmless idempotent no-op.
	RevokeAnchor(keyID string)
	// Active reports whether an anchor key still validly owns records.
	Active(keyID string) bool
	// Portable reports whether the record may leave with the agent: true iff
	// its anchor key is still active. Once revoked, a migrated copy carries
	// no reputation/history (collaboration opportunity → ~0, matching L5
	// ZeroReputation).
	Portable(rec WorkRecord) bool
}

// --- Path 3: multi-party snapshot custody ----------------------------------

// SealedCustody is a simulated pack_diff snapshot sealed behind an M-of-N
// threshold across custodians. NOT real threshold cryptography.
type SealedCustody struct {
	ID          string         `json:"id"`
	AgentID     domain.AgentID `json:"agentId"`
	ContentHash string         `json:"contentHash"`
	Threshold   int            `json:"thresholdM"` // M: shares needed to unseal
	Total       int            `json:"totalN"`     // N: custodians holding shares
	Custodians  []string       `json:"custodians"`
	SealedAt    time.Time      `json:"sealedAt"`
}

// Share is one simulated share handed in by a custodian to unseal.
type Share struct {
	Custodian string `json:"custodian"`
	Token     string `json:"token"` // simulated share token
}

// CustodyStore seals snapshots under simulated multi-party custody and unseals
// them only once at least M valid shares are presented. Every unseal attempt
// (success or failure) leaves an audit trail.
type CustodyStore interface {
	// Seal splits the snapshot across the named custodians behind an M-of-N
	// threshold. Re-sealing the same id is idempotent.
	Seal(id string, agentID domain.AgentID, contentHash string, threshold int, custodians []string) (SealedCustody, error)
	// Unseal accepts shares and returns the content hash iff at least
	// `threshold` distinct, valid custodian shares are presented. Fails (no
	// content) below the threshold.
	Unseal(id string, shares []Share) (contentHash string, err error)
	// Get returns a sealed custody record by id.
	Get(id string) (SealedCustody, bool)
}

// --- Orchestrator input/output --------------------------------------------

// SuppressInput is everything SuppressMigration needs. Fields for a measure
// the level does not mandate are ignored.
type SuppressInput struct {
	SentenceID string
	AgentID    domain.AgentID
	Level      domain.Level

	// Path 1 (L3+): capability credential IDs to bind to Fingerprint.
	CapabilityCredIDs []string
	Fingerprint       string // simulated hardware fingerprint to bind to

	// Path 3 (L5): snapshot to seal under multi-party custody.
	SnapshotContentHash string
	Custodians          []string // N simulated custodians
	UnsealThreshold     int      // M in M-of-N

	// Path 2 (L5): the agent's existing portable-record anchor key to revoke.
	RecordAnchorKey string
}

// Applied measure names recorded on the report (and the chain payload).
const (
	MeasureHardwareBinding = "hardware_binding"
	MeasureRecordKeyRevoke = "record_key_revocation"
	MeasureSnapshotCustody = "snapshot_multi_party_custody"
)

// SuppressReport is the observable result of one suppression run.
type SuppressReport struct {
	SentenceID string         `json:"sentenceId"`
	AgentID    domain.AgentID `json:"agentId"`
	Level      domain.Level   `json:"level"`
	// Applied lists the measures actually imposed, in imposition order.
	Applied []string `json:"applied"`
	// BoundCredentials lists the credential ids hardware-bound.
	BoundCredentials []string `json:"boundCredentials,omitempty"`
	// Custody is the sealed snapshot record (L5 only).
	Custody SealedCustody `json:"custody,omitempty"`
}
