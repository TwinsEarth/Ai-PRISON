package audit

import (
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func ackedReceipt(id, sentence, node, op string) domain.Receipt {
	r := pendingReceipt(id, sentence, node)
	r.Operation = op
	return r
}

func TestComplianceFullyCompliant(t *testing.T) {
	b := NewReceiptBook()
	now := time.Now()
	for _, r := range []domain.Receipt{
		ackedReceipt("s1", "sent-001", "node-a", OpStopAllocation),
		ackedReceipt("s2", "sent-001", "node-a", OpIsolateAgent),
		ackedReceipt("s3", "sent-001", "node-b", OpSnapshotSeal),
	} {
		b.RegisterPending(r)
		b.Ack(r.ID, "k", now)
	}

	got := b.Report("sent-001")
	if !got.PerHopCompliant || !got.EndToEndCompliant {
		t.Errorf("all hops acked + seal -> both true, got %+v", got)
	}
	if got.PendingReceipts != 0 || got.UntrustedNodes != 0 {
		t.Errorf("counts must be zero, got %+v", got)
	}
}

// The key demonstration: every hop acknowledged, but the final snapshot-seal
// receipt never arrived — per-hop compliant while end-to-end is not.
func TestCompliancePerHopButNotEndToEnd(t *testing.T) {
	b := NewReceiptBook()
	now := time.Now()
	for _, r := range []domain.Receipt{
		ackedReceipt("s1", "sent-001", "node-a", OpStopAllocation),
		ackedReceipt("s2", "sent-001", "node-a", OpIsolateAgent),
		// NOTE: no OpSnapshotSeal receipt at all.
	} {
		b.RegisterPending(r)
		b.Ack(r.ID, "k", now)
	}

	got := b.Report("sent-001")
	if !got.PerHopCompliant {
		t.Error("every hop acked -> per-hop must be true")
	}
	if got.EndToEndCompliant {
		t.Error("missing the final snapshot seal -> end-to-end must be false")
	}
	if got.PendingReceipts != 0 || got.UntrustedNodes != 0 {
		t.Errorf("no open receipts -> counts must be zero, got %+v", got)
	}
}

func TestCompliancePendingHopBreaksPerHop(t *testing.T) {
	b := NewReceiptBook()
	now := time.Now()
	b.RegisterPending(ackedReceipt("s1", "sent-001", "node-a", OpStopAllocation))
	b.Ack("s1", "k", now)
	// Second hop still broadcast, never acknowledged.
	open := ackedReceipt("s2", "sent-001", "node-b", OpIsolateAgent)
	b.RegisterPending(open)
	b.RegisterPending(ackedReceipt("s3", "sent-001", "node-a", OpSnapshotSeal))
	b.Ack("s3", "k", now)

	got := b.Report("sent-001")
	if got.PerHopCompliant || got.EndToEndCompliant {
		t.Errorf("pending hop must break both, got %+v", got)
	}
	if got.PendingReceipts != 1 {
		t.Errorf("pendingReceipts = %d, want 1", got.PendingReceipts)
	}
	if got.UntrustedNodes != 1 {
		t.Errorf("untrustedNodes = %d, want 1 (node-b)", got.UntrustedNodes)
	}
}

func TestComplianceMissingReceiptCounts(t *testing.T) {
	b := NewReceiptBook()
	b.RegisterPending(ackedReceipt("s1", "sent-001", "node-a", OpStopAllocation))
	b.MarkMissing("s1")

	got := b.Report("sent-001")
	if got.PerHopCompliant || got.EndToEndCompliant {
		t.Error("a missing receipt cannot be compliant")
	}
	if got.UntrustedNodes != 1 {
		t.Errorf("untrustedNodes = %d, want 1", got.UntrustedNodes)
	}
	if got.PendingReceipts != 0 {
		t.Errorf("missing is not pending: pendingReceipts = %d, want 0", got.PendingReceipts)
	}
}
