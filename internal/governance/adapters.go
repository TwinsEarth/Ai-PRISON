package governance

import (
	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

// EventSink is the append side of the judgement hash chain. It mirrors the
// small interface used across the enforcement layers so governance never
// hard-couples to *audit.Chain directly; production wiring uses
// NewEventSink, tests substitute fakes.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// chainEventSink adapts the real *audit.Chain to EventSink.
type chainEventSink struct{ chain *audit.Chain }

// NewEventSink adapts the judgement hash chain to the governance EventSink.
func NewEventSink(chain *audit.Chain) EventSink { return chainEventSink{chain: chain} }

func (s chainEventSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }

// PrincipalResolver resolves the human principal (人类委托人) party ID that
// stands behind an agent. It is the single authority used to decide who may
// file an appeal: only the registered principal of the sentenced agent can.
type PrincipalResolver interface {
	// PrincipalPartyID returns the human principal party ID registered for
	// agentID, and ok=false when the agent is unknown.
	PrincipalPartyID(agentID domain.AgentID) (partyID string, ok bool)
}

// agentPrincipalLookup adapts anything that can return a domain.Agent (for
// example internal/enforcement/identity.Service.GetAgent) to a
// PrincipalResolver. It is a structural adapter so governance does not import
// the identity package just to look up a principal.
type agentPrincipalLookup struct {
	Agents interface {
		GetAgent(domain.AgentID) (domain.Agent, bool)
	}
}

// NewPrincipalResolver adapts an agent registry exposing GetAgent (such as an
// *identity.Service) to a PrincipalResolver.
func NewPrincipalLookup(agents interface {
	GetAgent(domain.AgentID) (domain.Agent, bool)
}) PrincipalResolver {
	return agentPrincipalLookup{Agents: agents}
}

func (l agentPrincipalLookup) PrincipalPartyID(agentID domain.AgentID) (string, bool) {
	a, ok := l.Agents.GetAgent(agentID)
	if !ok {
		return "", false
	}
	return string(a.Principal.ID), true
}

// noopEventSink drops every event. Used only when no chain is wired; tests
// normally wire the real *audit.Chain.
type noopEventSink struct{}

func (noopEventSink) Append(ev domain.Event) domain.Event { return ev }
