package audit

import (
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func pendingReceipt(id, sentence, node string) domain.Receipt {
	return domain.Receipt{
		ID:          id,
		Operation:   OpStopAllocation,
		SentenceID:  sentence,
		NodeID:      node,
		BeforeState: "hash-before-1",
		AfterState:  "hash-after-1",
	}
}

func TestSimulatedSignatureVariesWithContent(t *testing.T) {
	base := SimulatedNodeSignature("node-key-a", OpStopAllocation, "b", "a")

	if base == "" {
		t.Fatal("signature must not be empty")
	}
	if got := SimulatedNodeSignature("node-key-b", OpStopAllocation, "b", "a"); got == base {
		t.Error("different node keys must produce different signatures")
	}
	if got := SimulatedNodeSignature("node-key-a", OpReclaimResource, "b", "a"); got == base {
		t.Error("different operations must produce different signatures")
	}
	if got := SimulatedNodeSignature("node-key-a", OpStopAllocation, "B", "a"); got == base {
		t.Error("different before-state hashes must produce different signatures")
	}
	if got := SimulatedNodeSignature("node-key-a", OpStopAllocation, "b", "A"); got == base {
		t.Error("different after-state hashes must produce different signatures")
	}
}

func TestReceiptLifecyclePendingAckedMissing(t *testing.T) {
	b := NewReceiptBook()
	r := b.RegisterPending(pendingReceipt("r1", "sent-001", "node-a"))
	if r.Status != domain.ReceiptPending || r.Acknowledged() {
		t.Errorf("fresh receipt must be pending, got %+v", r)
	}

	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	acked, found := b.Ack("r1", "node-key-a", now)
	if !found || !acked.Acknowledged() || acked.Status != domain.ReceiptAcked {
		t.Fatalf("Ack must flip to acked with ackedAt, got %+v", acked)
	}
	if acked.Signature == "" {
		t.Error("acked receipt must carry the simulated signature")
	}

	// A second receipt never acknowledges -> marked missing.
	b.RegisterPending(pendingReceipt("r2", "sent-001", "node-b"))
	missing, found := b.MarkMissing("r2")
	if !found || missing.Status != domain.ReceiptMissing {
		t.Fatalf("MarkMissing must flip to missing, got %+v", missing)
	}

	if _, found := b.Ack("nope", "k", now); found {
		t.Error("unknown receipt must not ack")
	}
	if _, found := b.MarkMissing("nope"); found {
		t.Error("unknown receipt must not be markable missing")
	}
}

func TestUntrustedNodesAreThoseWithoutAck(t *testing.T) {
	b := NewReceiptBook()
	b.RegisterPending(pendingReceipt("r1", "sent-001", "node-a"))
	b.RegisterPending(pendingReceipt("r2", "sent-001", "node-b"))
	b.RegisterPending(pendingReceipt("r3", "sent-002", "node-c"))

	b.Ack("r1", "k", time.Now()) // node-a clean
	b.MarkMissing("r2")          // node-b untrusted

	got := b.UntrustedNodes()
	// node-c still pending -> untrusted too; node-a acked -> trusted.
	if len(got) != 2 || got[0] != "node-b" || got[1] != "node-c" {
		t.Errorf("untrusted = %v, want [node-b node-c]", got)
	}
}
