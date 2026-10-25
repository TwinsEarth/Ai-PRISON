package resource

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/revocation"
)

// This file adapts the real internal/revocation and internal/audit types to
// the small injected sinks declared in types.go. Keeping the adapters here
// (rather than importing the concrete types across the package) means the
// resource package stays testable with fakes while production code wires the
// authoritative revocation set, judgement chain and receipt book.

type setSink struct{ set *revocation.Set }

// NewRevocationSetSink adapts the global revocation set to RevocationSink.
func NewRevocationSetSink(set *revocation.Set) RevocationSink { return setSink{set: set} }

func (s setSink) RevokeResource(id string, agentID domain.AgentID, sentenceID, reason string, holdingNodes []string) {
	s.set.Revoke(revocation.Entry{
		ID:         id,
		AgentID:    agentID,
		Kind:       domain.CredentialResource,
		SentenceID: sentenceID,
		Reason:     reason,
		RevokedAt:  time.Now().UTC(),
	}, holdingNodes)
}

type chainSink struct{ chain *audit.Chain }

// NewChainSink adapts the judgement hash chain to EventSink.
func NewChainSink(chain *audit.Chain) EventSink { return chainSink{chain: chain} }

func (s chainSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }

type bookSink struct{ book *audit.ReceiptBook }

// NewReceiptBookSink adapts the execution receipt book to ReceiptSink.
func NewReceiptBookSink(book *audit.ReceiptBook) ReceiptSink { return bookSink{book: book} }

func (s bookSink) RegisterPending(r domain.Receipt) domain.Receipt {
	return s.book.RegisterPending(r)
}
