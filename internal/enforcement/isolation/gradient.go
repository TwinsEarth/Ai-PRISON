package isolation

import (
	"fmt"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Gradient owns the four simulated backends and the level→backend mapping.
// The mapping is deliberately derived from each level's authoritative
// domain.LevelProfile.Sandbox field, so it cannot drift out of lock-step with
// internal/domain/level.go.
type Gradient struct {
	byName map[domain.SandboxBackend]Backend
	order  []domain.SandboxBackend // lightest → heaviest
}

// NewGradient registers the four simulated backends on the strength continuum.
func NewGradient() *Gradient {
	g := &Gradient{
		byName: make(map[domain.SandboxBackend]Backend, len(registeredBackends)),
	}
	for _, b := range registeredBackends {
		g.byName[b.Name()] = b
		g.order = append(g.order, b.Name())
	}
	return g
}

// Recommended returns the backend the level profile mandates. It never maps
// an agent to the heaviest sandbox on its own: L0–L1 stay on the lightweight
// Wasmtime/container tier, and heavier containment arrives only via
// EscalationPath.
func (g *Gradient) Recommended(level domain.Level) (Backend, error) {
	p, err := level.Profile()
	if err != nil {
		return nil, err
	}
	b, ok := g.byName[p.Sandbox]
	if !ok {
		return nil, fmt.Errorf("isolation: no backend registered for %s (level %s)", p.Sandbox, level)
	}
	if !b.CanHost(level) {
		return nil, fmt.Errorf("isolation: backend %s cannot host level %s", p.Sandbox, level)
	}
	return b, nil
}

// strengthIndex returns the rank of a backend on the continuum (-1 unknown).
func (g *Gradient) strengthIndex(name domain.SandboxBackend) int {
	for i, n := range g.order {
		if n == name {
			return i
		}
	}
	return -1
}

// EscalationPath returns the ordered backends an agent must pass through when
// moving from the `from` backend to `to`. Escalation is strictly stepwise:
// every rung between the two is included (e.g. Wasmtime → MicroVM passes
// through Docker), and a downward or sideways move is rejected. The returned
// slice includes `to` but not `from` — the rungs the migration must establish.
func (g *Gradient) EscalationPath(from, to domain.SandboxBackend) ([]Backend, error) {
	fromIdx := g.strengthIndex(from)
	toIdx := g.strengthIndex(to)
	if fromIdx < 0 || toIdx < 0 {
		return nil, fmt.Errorf("isolation: unknown backend on the gradient (%s -> %s)", from, to)
	}
	if toIdx <= fromIdx {
		return nil, fmt.Errorf("isolation: isolation may only escalate upward, never downgrade in place (%s -> %s)", from, to)
	}
	path := make([]Backend, 0, toIdx-fromIdx)
	for i := fromIdx + 1; i <= toIdx; i++ {
		path = append(path, g.byName[g.order[i]])
	}
	return path, nil
}

// Backends returns the registered backends in lightest-to-heaviest order
// (useful for listing / tests).
func (g *Gradient) Backends() []Backend {
	out := make([]Backend, len(g.order))
	for i, n := range g.order {
		out[i] = g.byName[n]
	}
	return out
}
