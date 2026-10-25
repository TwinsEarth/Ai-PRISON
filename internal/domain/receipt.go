package domain

import "time"

// ReceiptStatus is the acknowledgement state of an enforcement operation.
type ReceiptStatus string

const (
	ReceiptPending ReceiptStatus = "pending" // broadcast but not acknowledged
	ReceiptAcked   ReceiptStatus = "acked"   // node signed an acknowledgement
	ReceiptMissing ReceiptStatus = "missing" // node failed to acknowledge -> untrusted
)

// Receipt is a signed acknowledgement that one enforcement node actually
// carried out an operation. "Broadcast" is never treated as "delivered".
type Receipt struct {
	ID          string        `json:"id"`
	Operation   string        `json:"operation"`
	SentenceID  string        `json:"sentenceId"`
	NodeID      string        `json:"nodeId"`
	Status      ReceiptStatus `json:"status"`
	BeforeState string        `json:"beforeStateHash"`
	AfterState  string        `json:"afterStateHash"`
	Signature   string        `json:"signature"`
	IssuedAt    time.Time     `json:"issuedAt"`
	AckedAt     *time.Time    `json:"ackedAt,omitempty"`
}

// Acknowledged reports whether the node confirmed execution.
func (r Receipt) Acknowledged() bool { return r.Status == ReceiptAcked && r.AckedAt != nil }
