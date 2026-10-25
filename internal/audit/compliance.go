package audit

import (
	"fmt"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/pkg/contracts"
)

// Report evaluates, for one sentence, the two distinct compliance questions
// the design document requires:
//
//   - per-hop: every single execution step has an acked receipt (each hop
//     individually conforms to the sentence);
//   - end-to-end: per-hop holds AND the final state snapshot has been sealed
//     and acked, so the *final* runtime state matches the sentence's target.
//
// The two can disagree: every step may be acknowledged while the final
// snapshot seal is missing — per-hop compliant, end-to-end non-compliant. Per
// the design, such a gap points at the verification granularity, not at one
// "bad" node.
func (b *ReceiptBook) Report(sentenceID string) contracts.ComplianceReport {
	report := contracts.ComplianceReport{SentenceID: sentenceID}

	receipts := b.bySentence(sentenceID)
	perHop := true
	hasSeal := false
	pending := 0
	untrusted := map[string]struct{}{}

	for _, r := range receipts {
		if r.Status != domain.ReceiptAcked {
			perHop = false
			untrusted[r.NodeID] = struct{}{}
			if r.Status == domain.ReceiptPending {
				pending++
			}
			continue
		}
		if r.Operation == OpSnapshotSeal {
			hasSeal = true
		}
	}

	report.PerHopCompliant = perHop
	report.EndToEndCompliant = perHop && hasSeal
	report.PendingReceipts = pending
	report.UntrustedNodes = len(untrusted)

	switch {
	case perHop && !hasSeal:
		report.Notes = "every hop acknowledged, but the final snapshot-seal receipt is missing: per-hop compliance without end-to-end compliance"
	case !perHop:
		report.Notes = fmt.Sprintf("%d pending receipts and/or missing receipts: not all hops executed", pending)
	default:
		report.Notes = "all hops acknowledged and the final snapshot sealed"
	}
	return report
}
