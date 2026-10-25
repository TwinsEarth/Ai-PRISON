package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Record kinds understood by the durable governance ledger. Wiring layers
// (internal/prison) project domain objects onto these kinds; the store itself
// is payload-agnostic and stores payload as opaque canonical JSON.
const (
	// KindAuditEvent carries one full domain.Event (the judgement hash chain),
	// replayed back into an audit.Chain on restart.
	KindAuditEvent = "audit.event"
	// KindRevocationEntry carries one revocation.Entry (without acks).
	KindRevocationEntry = "revocation.entry"
	// KindRevocationAck carries a (credentialID, nodeID) acknowledgement.
	KindRevocationAck = "revocation.ack"
	// KindReceipt carries the state of one execution receipt.
	KindReceipt = "receipt"
	// KindSnapshot carries sealed-environment snapshot metadata.
	KindSnapshot = "snapshot.metadata"
	// KindAgent carries an agent registration (DID, AgentCard, principal, nodes).
	KindAgent = "agent.registration"
	// KindCase carries an adjudicated case (governance state, votes, stage).
	KindCase = "case"
	// KindSentence carries one issued sentence.
	KindSentence = "sentence"
	// KindRuntimeStatus carries the orchestrator's per-agent runtime status.
	KindRuntimeStatus = "runtime.status"
	// KindRecovery carries a restoration / recovery-drill outcome.
	KindRecovery = "recovery"
)

// Record is one immutable, append-only ledger row. Seq is a global, zero-based
// monotonic ordinal; PrevHash links to the previous record's Hash (empty for
// the genesis record). Hash binds seq, kind, timestamp, payload and PrevHash,
// so any later mutation — content, order, truncation — is detectable.
type Record struct {
	Seq      uint64          `json:"seq"`
	Kind     string          `json:"kind"`
	At       time.Time       `json:"at"`
	Payload  json.RawMessage `json:"payload"`
	PrevHash string          `json:"prevHash"`
	Hash     string          `json:"hash"`
}

// canonicalFrame is the deterministic, order-independent form hashed for each
// record. It is an array (not an object) so field ordering is fixed, and the
// timestamp is rendered in RFC3339Nano UTC exactly as it is stored.
type canonicalFrame struct {
	Seq      uint64
	Kind     string
	At       string
	PrevHash string
	Payload  []byte
}

// marshalPayload turns any payload into canonical JSON bytes. Values already
// encoded as json.RawMessage pass through; structs marshal in their fixed
// field order (deterministic for a given type); nil becomes JSON null.
func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage("null"), nil
	}
	if raw, ok := payload.(json.RawMessage); ok {
		if len(raw) == 0 {
			return json.RawMessage("null"), nil
		}
		// Normalise so stored bytes are valid JSON; re-marshal of a valid
		// RawMessage round-trips as-is.
		if json.Valid(raw) {
			return raw, nil
		}
		return nil, &InvalidPayloadError{Reason: "payload is not valid JSON"}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, &InvalidPayloadError{Reason: err.Error()}
	}
	return b, nil
}

// InvalidPayloadError is returned when a payload cannot be JSON-encoded.
type InvalidPayloadError struct{ Reason string }

func (e *InvalidPayloadError) Error() string { return "store: invalid payload: " + e.Reason }

// hashRecord computes the SHA-256 hex digest binding the record fields.
func hashRecord(seq uint64, kind string, at time.Time, prevHash string, payload []byte) string {
	frame := canonicalFrame{
		Seq:      seq,
		Kind:     kind,
		At:       at.UTC().Format(time.RFC3339Nano),
		PrevHash: prevHash,
		Payload:  payload,
	}
	// An array form is used so the digest never depends on Go struct field
	// names/order; the values stay positional.
	b, err := json.Marshal([5]any{
		frame.Seq,
		frame.Kind,
		frame.At,
		frame.PrevHash,
		json.RawMessage(frame.Payload),
	})
	if err != nil {
		// All components are JSON-safe in practice.
		panic("store: cannot hash record: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// storeRecord is the backend-internal construction shared by both backends:
// it assigns seq, normalises the timestamp and computes the digest.
func storeRecord(seq uint64, prevHash string, kind string, at time.Time, payload json.RawMessage) Record {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Record{
		Seq:      seq,
		Kind:     kind,
		At:       at.UTC(),
		Payload:  payload,
		PrevHash: prevHash,
		Hash:     hashRecord(seq, kind, at, prevHash, payload),
	}
}

// Store is the append-only durable ledger contract. Both MemoryStore and
// FileStore implement it.
//
// Append adds one record with the next sequence number. With a
// SyncEveryAppend FileStore the record is durably on disk before Append
// returns; otherwise Sync is the durability boundary.
//
// Replay visits every record in seq order. A visit error aborts iteration.
// Opening a FileStore (and Replay on it) verifies the hash chain; a tampered
// or torn ledger yields an *IntegrityError.
type Store interface {
	// Append adds one record carrying payload (any JSON-encodable value).
	Append(ctx context.Context, kind string, payload any) (Record, error)
	// Replay visits all records in seq order.
	Replay(ctx context.Context, visit func(Record) error) error
	// Sync flushes buffered records durably (no-op for MemoryStore and for a
	// FileStore that is already SyncEveryAppend).
	Sync(ctx context.Context) error
	// Len is the number of logical records currently held.
	Len() int
	// Path returns the data directory for FileStore, or "" for MemoryStore.
	Path() string
	// Close flushes and releases resources.
	Close() error
}
