// Package revocation implements the global, append-only revocation set: the
// authoritative read-side gate consulted by domain.Credential.IsUsable
// *before* anything else — before local state, before the validity window.
//
// This is an in-memory, simulated implementation. There is no real network
// distribution, no real key ceremony and no real enforcement backend; the
// "holding nodes" and their acknowledgements model the cross-node replication
// protocol of the design document, locally. Frontier capabilities (real
// cluster-wide revocation propagation) are interfaces / memory simulations
// here, never real controls.
package revocation

import (
	"sort"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Entry mirrors schemas/revocation-entry.schema.json. Presence of an entry
// here means the credential MUST be rejected regardless of its validity
// window — this is the "revoked but not expired" cache window the design
// document calls out as a cardinal bug.
type Entry struct {
	ID             string                `json:"id"`
	AgentID        domain.AgentID        `json:"agentId"`
	Kind           domain.CredentialKind `json:"kind"`
	SentenceID     string                `json:"sentenceId"`
	Reason         string                `json:"reason"`
	RevokedAt      time.Time             `json:"revokedAt"`
	Enforced       bool                  `json:"enforced"`
	AcknowledgedBy []string              `json:"acknowledgedBy,omitempty"`
}

// record is the internal, mutable bookkeeping row for one revocation. The
// embedded Entry is what callers see; required/acked model the simulated
// replication acknowledgements and never leave the set untyped.
type record struct {
	Entry
	order    uint64
	required map[string]struct{}
	acked    map[string]struct{}
}

// Set is the global revocation set. Writes are append-only and idempotent;
// reads never mutate state. There is deliberately no Remove method: once a
// credential is revoked it stays revoked for the life of the set — appeal
// overturns and rehabilitation restores are separate, logged flows, not a
// deletion of history.
type Set struct {
	mu      sync.RWMutex
	order   uint64
	records []*record
	byID    map[string]*record
}

// NewSet returns an empty, ready-to-use revocation set.
func NewSet() *Set {
	return &Set{byID: make(map[string]*record)}
}

// Revoke writes entry into the revocation set. It is idempotent: a second
// write for the same ID is a no-op and returns the already-stored entry
// untouched.
//
// holdingNodes lists the enforcement nodes that must acknowledge before the
// revocation is considered enforced across the simulated cluster. An empty
// list means the revocation takes effect locally and is marked enforced
// immediately (vacuously all nodes confirmed).
func (s *Set) Revoke(entry Entry, holdingNodes []string) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byID[entry.ID]; ok {
		return existing.Entry
	}
	if entry.RevokedAt.IsZero() {
		entry.RevokedAt = time.Now().UTC()
	}
	req := make(map[string]struct{}, len(holdingNodes))
	for _, n := range holdingNodes {
		if n != "" {
			req[n] = struct{}{}
		}
	}
	rec := &record{
		Entry:    entry,
		order:    s.order,
		required: req,
		acked:    make(map[string]struct{}),
	}
	rec.Enforced = len(req) == 0
	s.order++
	s.records = append(s.records, rec)
	s.byID[entry.ID] = rec
	return rec.Entry
}

// IsRevoked reports whether id is present in the revocation set. It is the
// domain.RevocationChecker implementation and uses only a read lock: a query
// must never modify state.
func (s *Set) IsRevoked(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.byID[id]
	return ok
}

// AckNode records that nodeID confirmed the revocation of credentialID on its
// local simulated copy. It returns found=false for an unknown credential.
// When every required node has acknowledged, the entry's Enforced flag flips
// to true and AcknowledgedBy is rebuilt; acknowledging a node twice is
// harmless.
func (s *Set) AckNode(credentialID, nodeID string) (found bool, enforcedNow bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[credentialID]
	if !ok {
		return false, false
	}
	rec.acked[nodeID] = struct{}{}
	rec.AcknowledgedBy = sortedKeys(rec.acked)
	rec.Enforced = s.isEnforced(rec)
	return true, rec.Enforced
}

// isEnforced must be called with the write lock held.
func (s *Set) isEnforced(rec *record) bool {
	for n := range rec.required {
		if _, ok := rec.acked[n]; !ok {
			return false
		}
	}
	return true
}

// UnacknowledgedNodes returns the required nodes that have not yet confirmed
// credentialID. This is the input the v0.6 group-freeze (连带冻结) flow
// consumes: nodes that never acknowledged a revocation are not trusted, and
// the agents running on them are frozen as a group. found=false means the
// credential is not revoked here.
func (s *Set) UnacknowledgedNodes(credentialID string) (nodes []string, found bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byID[credentialID]
	if !ok {
		return nil, false
	}
	for n := range rec.required {
		if _, done := rec.acked[n]; !done {
			nodes = append(nodes, n)
		}
	}
	sort.Strings(nodes)
	return nodes, true
}

// List returns the revocation entries in write order. Each returned entry is
// a detached snapshot (including the acknowledgement list) suitable for
// cross-node replication export; mutating it does not change the set.
func (s *Set) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.records))
	for i, rec := range s.records {
		e := rec.Entry
		if len(rec.acked) > 0 {
			e.AcknowledgedBy = sortedKeys(rec.acked)
		}
		out[i] = e
	}
	return out
}

// Get returns a detached snapshot of one revocation entry.
func (s *Set) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byID[id]
	if !ok {
		return Entry{}, false
	}
	e := rec.Entry
	e.AcknowledgedBy = sortedKeys(rec.acked)
	return e, true
}

// Len returns the number of revoked credentials.
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// sortedKeys renders a set as a sorted, deterministic slice.
func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
