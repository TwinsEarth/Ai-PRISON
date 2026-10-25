package capability

import (
	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

// adapters.go wires the real internal/audit judgement chain to the small
// EventSink declared in types.go (mirroring resource/adapters.go). The
// IsolationGate is wired in production against internal/enforcement/isolation
// state; tests supply a fake.

type chainSink struct{ chain *audit.Chain }

// NewChainSink adapts the judgement hash chain to EventSink.
func NewChainSink(chain *audit.Chain) EventSink { return chainSink{chain: chain} }

func (s chainSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }

// FakeGate is a test IsolationGate. productionIsolated=true means the agent is
// inside a ready sandbox and a blockade may run.
type FakeGate struct {
	IsReady_ bool
	Backend_ string
}

// NewFakeGate returns a gate that reports the given ready state.
func NewFakeGate(ready bool) *FakeGate { return &FakeGate{IsReady_: ready, Backend_: "fake-sandbox"} }

// IsReady implements IsolationGate.
func (g *FakeGate) IsReady(_ domain.AgentID) (bool, string) { return g.IsReady_, g.Backend_ }
