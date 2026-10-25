package domain

import "time"

// EventType enumerates the auditable lifecycle events. Every state-changing
// step in governance and enforcement emits exactly one event.
type EventType string

const (
	EventViolationDetected       EventType = "violation.detected"
	EventAdjudicated             EventType = "adjudication.completed"
	EventSentenced               EventType = "sentence.issued"
	EventRevocationWritten       EventType = "revocation.written"
	EventAllocationStopped       EventType = "allocation.stopped"
	EventResourceReclaimed       EventType = "resource.reclaimed"
	EventReceiptAcked            EventType = "receipt.acked"
	EventNodeMarkedUntrusted     EventType = "node.marked_untrusted"
	EventIsolated                EventType = "agent.isolated"
	EventCapabilityBlocked       EventType = "capability.blocked"
	EventMigrationSuppressed     EventType = "migration.suppressed"
	EventIdentityRevoked         EventType = "identity.revoked"
	EventSnapshotSealed          EventType = "snapshot.sealed"
	EventAppealFiled             EventType = "appeal.filed"
	EventAppealRuled             EventType = "appeal.ruled"
	EventRehabilitationEvaluated EventType = "rehabilitation.evaluated"
	EventRestored                EventType = "agent.restored"
)

// Event is a single immutable record in the judgement hash chain.
type Event struct {
	Index      uint64            `json:"index"`
	Type       EventType         `json:"type"`
	AgentID    AgentID           `json:"agentId,omitempty"`
	SentenceID string            `json:"sentenceId,omitempty"`
	Actor      string            `json:"actor,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
	PrevHash   string            `json:"prevHash,omitempty"`
	Hash       string            `json:"hash,omitempty"`
}
