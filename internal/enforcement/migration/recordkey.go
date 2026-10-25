package migration

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// MemRecordKeeper is the in-memory RecordKeeper. The anchor key is a simulated
// key id in a Go map — NOT a real key custody / PKI ecosystem. The point is
// bookkeeping: once the anchor key is revoked, a migrated copy cannot carry the
// agent's reputation and history.
type MemRecordKeeper struct {
	mu     sync.RWMutex
	active map[string]domain.AgentID // keyID -> owning agent (present = key live)
	next   atomic.Uint64
}

// NewMemRecordKeeper returns an empty keeper.
func NewMemRecordKeeper() *MemRecordKeeper {
	return &MemRecordKeeper{active: make(map[string]domain.AgentID)}
}

// Anchor issues a fresh simulated anchor key for the agent. The returned key id
// is what a WorkRecord must cite to be portable.
func (k *MemRecordKeeper) Anchor(agentID domain.AgentID) string {
	id := fmt.Sprintf("rec-key-%s-%d", agentID, k.next.Add(1))
	k.mu.Lock()
	k.active[id] = agentID
	k.mu.Unlock()
	return id
}

// RevokeAnchor voids an anchor key. This is the L5 step: the migrated copy
// still has the record bytes, but without a live anchor key the record is not
// accepted as the original identity's history — reputation does not inherit.
// Revoking an unknown key is a harmless idempotent no-op.
func (k *MemRecordKeeper) RevokeAnchor(keyID string) {
	k.mu.Lock()
	delete(k.active, keyID)
	k.mu.Unlock()
}

// Active reports whether an anchor key still owns records.
func (k *MemRecordKeeper) Active(keyID string) bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.active[keyID]
	return ok
}

// Portable reports whether the record may leave with the agent. It is true iff
// the cited anchor key is still live; after RevokeAnchor it is false, so the
// copy cannot inherit reputation/history (aligns with L5 ZeroReputation: a
// reputation-less agent has ~zero collaboration opportunity).
func (k *MemRecordKeeper) Portable(rec WorkRecord) bool {
	return k.Active(rec.AnchorKey)
}

// Len returns the number of live anchor keys (idempotency tests).
func (k *MemRecordKeeper) Len() int {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return len(k.active)
}
