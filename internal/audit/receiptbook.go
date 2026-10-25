package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Simulated receipt operations understood by the compliance evaluator.
const (
	OpStopAllocation  = "allocation.stop"  // new allocation frozen
	OpReclaimResource = "resource.reclaim" // gradual reclamation step
	OpIsolateAgent    = "agent.isolated"   // agent isolated
	OpSnapshotSeal    = "snapshot.seal"    // final state snapshot sealed
)

// SimulatedNodeSignature computes an HMAC-SHA256 over the operation and the
// before/after state hashes.
//
// NOTE: this is a SIMULATED signature, not a real cryptographic identity.
// There is no PKI, no node key ceremony and no real signing infrastructure in
// this prototype. The value exists only so that tampering with an
// acknowledged receipt (operation, before/after state) is detectable.
func SimulatedNodeSignature(nodeKey, operation, beforeState, afterState string) string {
	mac := hmac.New(sha256.New, []byte(nodeKey))
	fmt.Fprintf(mac, "%s|%s|%s", operation, beforeState, afterState)
	return hex.EncodeToString(mac.Sum(nil))
}

// ReceiptBook tracks every execution receipt from broadcast to acknowledgement.
// "Broadcast" is never treated as "delivered": a pending receipt is an open
// accountability question, not a success.
type ReceiptBook struct {
	mu       sync.RWMutex
	order    []string
	receipts map[string]*domain.Receipt
}

// NewReceiptBook returns an empty receipt book.
func NewReceiptBook() *ReceiptBook {
	return &ReceiptBook{receipts: make(map[string]*domain.Receipt)}
}

// RegisterPending records a receipt that has been broadcast to a holding node
// but not yet acknowledged. The status is forced to pending and issuedAt is
// filled when zero. Re-registering the same ID is a no-op.
func (b *ReceiptBook) RegisterPending(r domain.Receipt) domain.Receipt {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.receipts[r.ID]; ok {
		return *existing
	}
	r.Status = domain.ReceiptPending
	if r.IssuedAt.IsZero() {
		r.IssuedAt = time.Now().UTC()
	}
	cp := r
	b.receipts[r.ID] = &cp
	b.order = append(b.order, r.ID)
	return cp
}

// Ack finalises receiptID as executed by its node: it recomputes the
// simulated signature over the receipt's own operation and before/after
// state hashes, stores it, stamps ackedAt and flips status to acked.
// found=false means no such receipt.
func (b *ReceiptBook) Ack(receiptID, nodeKey string, now time.Time) (domain.Receipt, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.receipts[receiptID]
	if !ok {
		return domain.Receipt{}, false
	}
	r.Signature = SimulatedNodeSignature(nodeKey, r.Operation, r.BeforeState, r.AfterState)
	r.Status = domain.ReceiptAcked
	t := now.UTC()
	r.AckedAt = &t
	return *r, true
}

// MarkMissing transitions a receipt that never acknowledged (timed out) to
// missing. A missing receipt is the direct evidence that its node is not
// trustworthy.
func (b *ReceiptBook) MarkMissing(receiptID string) (domain.Receipt, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.receipts[receiptID]
	if !ok {
		return domain.Receipt{}, false
	}
	r.Status = domain.ReceiptMissing
	return *r, true
}

// Restore rebuilds one receipt exactly as it was recorded on the durable
// ledger (event-sourced recovery). Unlike RegisterPending it does NOT force
// the status to pending: an acked/missing receipt is restored verbatim so the
// untrusted-node judgement is identical after a restart. Insertion order is
// preserved on first sight; re-restoring the same ID replaces it in place.
// It is a pure addition used by the prison persistence wiring; the live paths
// (RegisterPending/Ack/MarkMissing) are unchanged.
func (b *ReceiptBook) Restore(r domain.Receipt) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.receipts[r.ID]; !ok {
		b.order = append(b.order, r.ID)
	}
	cp := r
	b.receipts[r.ID] = &cp
}

// Get returns a copy of one receipt.
func (b *ReceiptBook) Get(id string) (domain.Receipt, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.receipts[id]
	if !ok {
		return domain.Receipt{}, false
	}
	return *r, true
}

// All returns copies of all receipts in registration order.
func (b *ReceiptBook) All() []domain.Receipt {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]domain.Receipt, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.receipts[id])
	}
	return out
}

// bySentence returns copies of all receipts for one sentence.
func (b *ReceiptBook) bySentence(sentenceID string) []domain.Receipt {
	all := b.All()
	out := make([]domain.Receipt, 0, len(all))
	for _, r := range all {
		if r.SentenceID == sentenceID {
			out = append(out, r)
		}
	}
	return out
}

// UntrustedNodes returns the sorted IDs of every node holding at least one
// receipt that is NOT acked (pending or missing). Per the design these nodes
// are untrusted, and the agents running on them are frozen as a group
// (连带冻结, wired up in v0.6).
func (b *ReceiptBook) UntrustedNodes() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	set := make(map[string]struct{})
	for _, id := range b.order {
		r := b.receipts[id]
		if r.Status != domain.ReceiptAcked {
			set[r.NodeID] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// pendingCount returns how many receipts are still awaiting an ack.
func (b *ReceiptBook) pendingCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, id := range b.order {
		if b.receipts[id].Status == domain.ReceiptPending {
			n++
		}
	}
	return n
}
