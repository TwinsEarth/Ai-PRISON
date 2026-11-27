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
// payload JSON, RFC3339Nano UTC timestamp and prevHash. It returns an error
// rather than panicking so a malformed event fails as a diagnosable error.
func hashOf(ev domain.Event) (string, error) {
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
		return "", fmt.Errorf("audit: canonical marshal at index %d: %w", ev.Index, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Chain is a concurrent-safe, append-only judgement hash chain. Events are
// never mutated after Append; Verify re-derives every digest and every link.
type Chain struct {
	mu      sync.RWMutex
	entries []domain.Event // hot window; may be compacted
	digests []chainDigest  // digest-only index of every event ever appended
	base    uint64         // absolute index of entries[0]
	retain  int            // hot window size; 0 keeps every event resident
	fault   error          // sticky failure (e.g. unhashable event)
	clock   func() time.Time
}

// chainDigest is the compact per-event record retained for evicted events:
// enough to re-verify the index/link structure without holding the payload.
type chainDigest struct {
	Index    uint64
	Hash     string
	PrevHash string
}

// NewChain returns an empty chain anchored at GenesisPrevHash.
func NewChain() *Chain { return &Chain{} }

// WithRetention bounds how many recent events stay fully resident. Older
// events keep their digest and linkage in a compact index, so ticket Verify
// still detects reordering/relinking while memory stays bounded. 0 means
// unlimited (pre-1.9.5 behaviour).
func (c *Chain) WithRetention(n int) *Chain {
	if n >= 0 {
		c.retain = n
	}
	return c
}

// WithClock pins the default timestamp source used to stamp events that arrive
// with a zero Timestamp. Without it the chain falls back to wall-clock time;
// the orchestrator injects its configured clock here so an entire ledger,
// including chain events, is reproducible.
func (c *Chain) WithClock(f func() time.Time) *Chain {
	if f != nil {
		c.clock = f
	}
	return c
}

func (c *Chain) now() time.Time {
	if c.clock != nil {
		return c.clock().UTC()
	}
	return time.Now().UTC()
}

// Append stamps the event with its index, the previous entry's hash (or
// GenesisPrevHash for the head), computes and stores the digest, and returns
// the stored record. A zero Timestamp is filled with now (UTC); a supplied
// timestamp is normalised to UTC. Append is the only write path.
func (c *Chain) Append(ev domain.Event) domain.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fault != nil {
		// Fail closed: a poisoned chain accepts nothing and Verify surfaces why.
		return ev
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = c.now()
	} else {
		ev.Timestamp = ev.Timestamp.UTC()
	}
	total := uint64(len(c.digests))
	ev.Index = total
	ev.PrevHash = GenesisPrevHash
	if total > 0 {
		ev.PrevHash = c.digests[total-1].Hash
	}
	hash, err := hashOf(ev)
	if err != nil {
		c.fault = err
		return ev
	}
	ev.Hash = hash
	c.digests = append(c.digests, chainDigest{Index: ev.Index, Hash: ev.Hash, PrevHash: ev.PrevHash})
	c.entries = append(c.entries, ev)
	if c.retain > 0 && len(c.entries) > c.retain {
		drop := len(c.entries) - c.retain
		c.entries = c.entries[drop:]
		c.base += uint64(drop)
	}
	return ev
}

// Len returns the number of chained events (including compacted ones).
func (c *Chain) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.digests)
}

// Get returns a copy of the event at index when it is still resident, and
// false when out of range or compacted away (digests remain verifiable).
func (c *Chain) Get(index uint64) (domain.Event, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if index < c.base || index >= c.base+uint64(len(c.entries)) {
		return domain.Event{}, false
	}
	return c.entries[index-c.base], true
}

// Entries returns detached copies of the resident events in chain order.
func (c *Chain) Entries() []domain.Event {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.Event, len(c.entries))
	copy(out, c.entries)
	return out
}

// Verify re-checks every index/link from the compact digest index and
// re-derives content digests for the resident window. Compacted events are
// checked structurally; their payload digest is covered by the ledger's own
// per-record verification on replay.
func (c *Chain) Verify() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.fault != nil {
		return fmt.Errorf("audit: chain poisoned: %w", c.fault)
	}
	for i := range c.digests {
		if c.digests[i].Index != uint64(i) {
			return fmt.Errorf("audit: index %d out of order (got %d)", i, c.digests[i].Index)
		}
		wantPrev := GenesisPrevHash
		if i > 0 {
			wantPrev = c.digests[i-1].Hash
		}
		if c.digests[i].PrevHash != wantPrev {
			return fmt.Errorf("audit: broken chain link at index %d: prevHash %q want %q",
				i, c.digests[i].PrevHash, wantPrev)
		}
	}
	for i, ev := range c.entries {
		abs := c.base + uint64(i)
		got, err := hashOf(ev)
		if err != nil {
			return err
		}
		if got != ev.Hash || abs >= uint64(len(c.digests)) || got != c.digests[abs].Hash {
			return fmt.Errorf("audit: hash mismatch at index %d: stored %q recomputed %q", abs, ev.Hash, got)
		}
	}
	return nil
}

// VerifyEntries is the stateless verification primitive: it validates a
// detached chain (for example one exported by a remote simulated node) and
// reports the index of the first tampered record. It is exposed so tests and
// future cross-node checks do not need their own reimplementation.
func VerifyEntries(entries []domain.Event) error {
	for i, ev := range entries {
		got, err := hashOf(ev)
		if err != nil {
			return err
		}
		if got != ev.Hash {
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
