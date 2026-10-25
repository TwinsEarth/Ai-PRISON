package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// hashSnapshot computes the deterministic SHA-256 anchor of a simulated
// pack_diff snapshot. The canonical payload is key-sorted JSON over the
// agent, sentence, pre-reclaim quota and seal time; there is no real
// differential filesystem capture here.
func hashSnapshot(agentID string, sentenceID string, quota Quota, sealedAt time.Time) string {
	payload := map[string]any{
		"agentId":    string(agentID),
		"sentenceId": sentenceID,
		"sealedAt":   sealedAt.UTC().Format(time.RFC3339Nano),
		"quota":      quota,
		"simulated":  true,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		// Only strings/maps/int fields: marshalling cannot fail in practice.
		panic(fmt.Errorf("resource: snapshot canonical marshal: %w", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// MemSnapshotStore is the in-memory isolated snapshot storage. Sealing the
// same snapshot ID twice is an idempotent no-op (re-running Deprive must not
// mint a second evidence copy).
type MemSnapshotStore struct {
	mu    sync.Mutex
	items map[string]Snapshot
}

// NewMemSnapshotStore returns an empty snapshot store.
func NewMemSnapshotStore() *MemSnapshotStore {
	return &MemSnapshotStore{items: make(map[string]Snapshot)}
}

// Seal stores s. A zero ContentHash is computed from the snapshot's canonical
// payload; a zero SealedAt is filled with now (UTC).
func (s *MemSnapshotStore) Seal(snap Snapshot) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snap.ID == "" {
		// Deterministic per (agent, sentence): re-running Deprive for the same
		// sentence returns the already-sealed evidence copy instead of minting
		// a second snapshot.
		snap.ID = "snap-" + string(snap.AgentID) + "-" + snap.SentenceID
	}
	if existing, ok := s.items[snap.ID]; ok {
		return existing, nil
	}
	if snap.SealedAt.IsZero() {
		snap.SealedAt = time.Now().UTC()
	}
	if snap.ContentHash == "" {
		snap.ContentHash = hashSnapshot(string(snap.AgentID), snap.SentenceID, snap.Quota, snap.SealedAt)
	}
	s.items[snap.ID] = snap
	return snap, nil
}

// Get returns a sealed snapshot by ID.
func (s *MemSnapshotStore) Get(id string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.items[id]
	return snap, ok
}

// All returns detached copies of every sealed snapshot in seal order. It is a
// pure addition used by the persistence wiring to snapshot the evidence catalog
// onto the durable ledger; Seal/Get/Len behaviour is unchanged.
func (s *MemSnapshotStore) All() []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Snapshot, 0, len(s.items))
	for _, snap := range s.items {
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Restore re-seals a snapshot record read back from the durable ledger. It is
// idempotent (a re-run with the same ID returns the stored copy) and never
// recomputes an existing content hash.
func (s *MemSnapshotStore) Restore(snap Snapshot) (Snapshot, error) {
	return s.Seal(snap)
}

// Len returns the number of sealed snapshots (used by idempotency tests).
func (s *MemSnapshotStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
