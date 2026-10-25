package migration

import (
	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

// This file adapts the real internal/audit types to the small injected sinks
// declared in types.go, mirroring internal/enforcement/resource/adapters.go.
// Keeping adapters here means the migration package stays testable with fakes
// while production wires the authoritative judgement chain.
//
// Note: internal/revocation.Set already satisfies domain.RevocationChecker
// (IsRevoked), so hardware binding validation can take a *revocation.Set
// directly — no adapter is needed for that side.

type chainSink struct{ chain *audit.Chain }

// NewChainSink adapts the judgement hash chain to EventSink.
func NewChainSink(chain *audit.Chain) EventSink { return chainSink{chain: chain} }

func (s chainSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }
