package identity

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/revocation"
)

// This file adapts the real internal/revocation set, internal/audit chain and
// internal/audit receipt book to the small injected sinks declared in
// types.go — the same dependency-injection style used by
// internal/enforcement/resource/adapters.go. Keeping the adapters here means
// the identity package stays testable with fakes while production code wires
// the authoritative components.

type setSink struct{ set *revocation.Set }

// NewRevocationSink adapts the global revocation set to RevocationSink.
func NewRevocationSink(set *revocation.Set) RevocationSink { return setSink{set: set} }

func (s setSink) RevokeCredential(id string, kind domain.CredentialKind, agentID domain.AgentID, sentenceID, reason string, holdingNodes []string) {
	s.set.Revoke(revocation.Entry{
		ID:         id,
		AgentID:    agentID,
		Kind:       kind,
		SentenceID: sentenceID,
		Reason:     reason,
		RevokedAt:  time.Now().UTC(),
	}, holdingNodes)
}

type chainSink struct{ chain *audit.Chain }

// NewEventSink adapts the judgement hash chain to EventSink.
func NewEventSink(chain *audit.Chain) EventSink { return chainSink{chain: chain} }

func (s chainSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }

type bookController struct{ book *audit.ReceiptBook }

// NewReceiptController adapts the execution receipt book to ReceiptController.
// The book remains the single authority on which nodes are untrusted.
func NewReceiptController(book *audit.ReceiptBook) ReceiptController {
	return bookController{book: book}
}

func (c bookController) RegisterPending(r domain.Receipt) domain.Receipt {
	return c.book.RegisterPending(r)
}

func (c bookController) Ack(receiptID, nodeKey string, now time.Time) (domain.Receipt, bool) {
	return c.book.Ack(receiptID, nodeKey, now)
}

func (c bookController) UntrustedNodes() []string { return c.book.UntrustedNodes() }
