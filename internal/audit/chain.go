// Package audit implements the audit layer of AI Prison: the tamper-evident
// judgement hash chain, the execution receipt book, and the per-hop vs
// end-to-end compliance reporting.
//
// Everything here is an in-memory simulation. There is no real replicated
// ledger, no real network and no real cryptographic node identity: the
// "signatures" are simulated HMAC-SHA256 values and the chain lives in this
// process only. Frontier capabilities are interfaces / memory simulations,
// never real controls. The package is strictly defensive: traceability,
// verifiability and accountability — no escape, attack or weaponization.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// GenesisPrevHash is the fixed prevHash of the first chain entry, linking the
// chain to a known, well-defined starting point.
const GenesisPrevHash = ""

// canonicalEvent is the deterministic, key-sorted serialisation of an event
// for hashing. The wire Event type carries an omitempty Hash field; hashing
// must never depend on Go struct field order or on the hash being stored, so
// the canonical form is built explicitly and payloads are marshalled as a
// JSON object whose keys encoding/json always sorts lexicographically.
type canonicalEvent struct {
	Index      uint64            `json:"index"`
	Type       domain.EventType  `json:"type"`
	AgentID    domain.AgentID    `json:"agentId,omitempty"`
	SentenceID string            `json:"sentenceId,omitempty"`
	Actor      string            `json:"actor,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"`
	Timestamp  string            `json:"timestamp"` // RFC3339Nano, UTC
	PrevHash   string            `json:"prevHash"`
}

// hashOf computes the canonical SHA-256 hex digest of one event. The input is
// fully deterministic: index, type, agentId, sentenceId, actor, sorted-key
// payload JSON, RFC3339Nano UTC timestamp and prevHash.
func hashOf(ev domain.Event) string {
	canon := canonicalEvent{
		Index:      ev.Index,
		Type:       ev.Type,
		AgentID:    ev.AgentID,
		SentenceID: ev.SentenceID,
		Actor:      ev.Actor,
		Payload:    ev.Payload,
		Timestamp:  ev.Timestamp.UTC().Format(time.RFC3339Nano),
		PrevHash:   ev.PrevHash,
	}
	b, err := json.Marshal(canon)
	if err != nil {
		// canonicalEvent contains only strings/maps of strings; marshalling
		// cannot fail in practice. Panic loudly rather than store a bad hash.
		panic(fmt.Errorf("audit: canonical marshal: %w", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Chain is a concurrent-safe, append-only judgement hash chain. Events are
// never mutated after Append; Verify re-derives every digest and every link.
type Chain struct {
	mu      sync.RWMutex
	entries []domain.Event
}

// NewChain returns an empty chain anchored at GenesisPrevHash.
func NewChain() *Chain { return &Chain{} }

// Append stamps the event with its index, the previous entry's hash (or
// GenesisPrevHash for the head), computes and stores the digest, and returns
// the stored record. A zero Timestamp is filled with now (UTC); a supplied
// timestamp is normalised to UTC. Append is the only write path.
func (c *Chain) Append(ev domain.Event) domain.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	} else {
		ev.Timestamp = ev.Timestamp.UTC()
	}
	ev.Index = uint64(len(c.entries))
	ev.PrevHash = GenesisPrevHash
	if ev.Index > 0 {
		ev.PrevHash = c.entries[len(c.entries)-1].Hash
	}
	ev.Hash = hashOf(ev)
	c.entries = append(c.entries, ev)
	return ev
}

// Len returns the number of chained events.
func (c *Chain) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Get returns a copy of the event at index, and false when out of range.
func (c *Chain) Get(index uint64) (domain.Event, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if int(index) >= len(c.entries) {
		return domain.Event{}, false
	}
	return c.entries[index], true
}

// Entries returns detached copies of all events in chain order, suitable for
// cross-node verification.
func (c *Chain) Entries() []domain.Event {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.Event, len(c.entries))
	copy(out, c.entries)
	return out
}

// Verify re-computes every digest and re-checks every link, reporting the
// offending index on the first mismatch. An empty chain verifies cleanly.
func (c *Chain) Verify() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return VerifyEntries(c.entries)
}

// VerifyEntries is the stateless verification primitive: it validates a
// detached chain (for example one exported by a remote simulated node) and
// reports the index of the first tampered record. It is exposed so tests and
// future cross-node checks do not need their own reimplementation.
func VerifyEntries(entries []domain.Event) error {
	for i, ev := range entries {
		if got := hashOf(ev); got != ev.Hash {
			return fmt.Errorf("audit: hash mismatch at index %d: stored %q recomputed %q", i, ev.Hash, got)
		}
		wantPrev := GenesisPrevHash
		if i > 0 {
			wantPrev = entries[i-1].Hash
		}
		if ev.PrevHash != wantPrev {
			return fmt.Errorf("audit: broken chain link at index %d: prevHash %q want %q", i, ev.PrevHash, wantPrev)
		}
	}
	return nil
}
