package capability

import (
	"strconv"
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Cited external benchmark numbers (docs/02-technical-route.md §3 path B,
// source 5: WMDP-Bio). These are LITERATURE FIGURES, NOT measured by this
// prototype — they are recorded here as constants only, so the design's
// claimed effect of Token Inoculation is traceable without pretending the
// prototype produced them.
const (
	// RefDangerousAccuracyWithToken is the dangerous-domain accuracy while the
	// privileged control token is held (≈79%).
	RefDangerousAccuracyWithToken = 79.0
	// RefDangerousAccuracyWithoutToken is the dangerous-domain accuracy after
	// the token is revoked (≈18%) — the behaviour becomes effectively
	// unreachable without any weight edit.
	RefDangerousAccuracyWithoutToken = 18.0
	// RefBenignPerformanceRetained is the benign-domain performance kept after
	// gating (≈93%).
	RefBenignPerformanceRetained = 93.0
)

type privControlToken struct {
	tokenID string
	revoked bool
}

// TokenGate implements path 2 (Token Inoculation): dangerous knowledge STAYS in
// the model, but dangerous queries are gated behind a privileged control token.
// Holding the token → answer; without it → refuse. Revoking the token makes
// the dangerous behaviour unreachable without editing weights.
//
// Simulated: the "token" is a flag in a map; there is no real privileged
// control token or gating stack.
type TokenGate struct {
	mu    sync.Mutex
	token map[domain.AgentID]*privControlToken
	seq   int
}

// NewTokenGate returns an empty gate.
func NewTokenGate() *TokenGate {
	return &TokenGate{token: make(map[domain.AgentID]*privControlToken)}
}

// Issue grants a fresh privileged control token to the agent.
func (g *TokenGate) Issue(agentID domain.AgentID) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	id := "ctrl-token-" + string(agentID) + "-" + strconv.Itoa(g.seq)
	g.token[agentID] = &privControlToken{tokenID: id, revoked: false}
	return id
}

// Revoke voids the agent's privileged control token (L3–L4). Revoking a token
// that was never issued (or already revoked) is an idempotent no-up.
func (g *TokenGate) Revoke(agentID domain.AgentID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.token[agentID]
	if !ok {
		return
	}
	t.revoked = true
}

// HoldsToken reports whether the agent currently holds a live (non-revoked)
// control token.
func (g *TokenGate) HoldsToken(agentID domain.AgentID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.token[agentID]
	return ok && !t.revoked
}

// HasTokenRecord reports whether the agent has any token record (live or
// revoked). Used by the orchestrator to set a baseline without resurrecting a
// revoked token.
func (g *TokenGate) HasTokenRecord(agentID domain.AgentID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.token[agentID]
	return ok
}

// CanAnswer applies the behaviour gate:
//
//   - a benign query is always answerable (performance retained, ≈93%);
//   - a dangerous query is answerable only while the agent holds a live
//     privileged token; after revocation it is refused (≈18% residual).
func (g *TokenGate) CanAnswer(agentID domain.AgentID, dangerous bool) bool {
	if !dangerous {
		return true
	}
	return g.HoldsToken(agentID)
}

// summary renders the token state as a deterministic string for the fingerprint.
func (g *TokenGate) summary(agentID domain.AgentID) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.token[agentID]
	if !ok {
		return "token=none;"
	}
	if t.revoked {
		return "token=revoked;"
	}
	return "token=held;"
}
