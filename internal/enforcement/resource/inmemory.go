package resource

import (
	"fmt"
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// account is the mutable per-agent bookkeeping row behind the in-memory
// backend. It lives entirely in this process.
type account struct {
	quota    Quota
	startCPU int  // CPU quota at allocation time: the top rung
	steps    int  // planned gradual rungs; ReclaimStep walks exactly this many
	rung     int  // rungs already reclaimed (0..steps)
	stopped  bool // new allocations frozen after the revocation set write
}

// InMemoryBackend is the simulated Backend: it keeps one Quota per agent and
// walks the CPU quota down rung by rung. It is NOT real quota control — there
// are no cgroups, no kernel syscalls and no network effects.
type InMemoryBackend struct {
	mu       sync.Mutex
	checker  domain.RevocationChecker // consulted before EVERY allocation
	accounts map[domain.AgentID]*account
}

// NewInMemoryBackend returns a backend that consults checker before any
// allocation. checker may be nil (in which case no revocation gate applies —
// production code must always wire the real global revocation set).
func NewInMemoryBackend(checker domain.RevocationChecker) *InMemoryBackend {
	return &InMemoryBackend{
		checker:  checker,
		accounts: make(map[domain.AgentID]*account),
	}
}

// Allocate grants q, but only if the credential is absent from the global
// revocation set and new allocations are not frozen for the agent. The
// revocation check is deliberately the FIRST gate, mirroring
// domain.Credential.IsUsable: "revoked but not expired" must be rejected.
func (b *InMemoryBackend) Allocate(q Quota) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.checker != nil && b.checker.IsRevoked(q.CredentialID) {
		return fmt.Errorf("resource: credential %q is present in the global revocation set: allocation rejected (revocation checked before expiry/state)", q.CredentialID)
	}
	if acc, ok := b.accounts[q.AgentID]; ok && acc.stopped {
		return fmt.Errorf("resource: new allocation to agent %q is stopped after deprivation", q.AgentID)
	}

	acc, ok := b.accounts[q.AgentID]
	if !ok {
		acc = &account{quota: q, startCPU: q.CPUMillis, steps: DefaultSteps}
		b.accounts[q.AgentID] = acc
		return nil
	}
	acc.quota = q
	return nil
}

// Current returns the live quota for agentID.
func (b *InMemoryBackend) Current(agentID domain.AgentID) (Quota, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	if !ok {
		return Quota{}, false
	}
	return acc.quota, true
}

// StopAllocation freezes new allocations for the agent (design step 2).
func (b *InMemoryBackend) StopAllocation(agentID domain.AgentID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	if !ok {
		acc = &account{}
		b.accounts[agentID] = acc
	}
	acc.stopped = true
}

// AllocationStopped reports whether new allocations are frozen for the agent.
func (b *InMemoryBackend) AllocationStopped(agentID domain.AgentID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	return ok && acc.stopped
}

// PlanReclaim records the gradual downgrade plan before the first reclaim
// rung. It must be called once, after the pre-reclaim snapshot was sealed.
func (b *InMemoryBackend) PlanReclaim(agentID domain.AgentID, steps int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if steps <= 0 {
		return fmt.Errorf("resource: reclaim plan for %q needs at least 1 rung, got %d", agentID, steps)
	}
	acc, ok := b.accounts[agentID]
	if !ok {
		return fmt.Errorf("resource: no quota allocated to %q; cannot plan reclaim", agentID)
	}
	acc.steps = steps
	return nil
}

// ReclaimStep performs exactly one gradual downgrade rung: the CPU quota
// drops by startCPU/steps. The final rung lands exactly on zero, so the walk
// always contains multiple intermediate states and never an instant jump from
// startCPU to zero. Calling it after the quota reached zero is an idempotent
// no-op, so re-running Deprive is safe.
func (b *InMemoryBackend) ReclaimStep(agentID domain.AgentID) (Quota, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	if !ok {
		return Quota{}, fmt.Errorf("resource: agent %q has no allocated quota", agentID)
	}
	if acc.rung >= acc.steps {
		// Already fully reclaimed: idempotent. Never panic, never re-zero.
		return acc.quota, nil
	}
	acc.rung++
	step := acc.startCPU / acc.steps
	next := acc.startCPU - acc.rung*step
	if acc.rung == acc.steps {
		next = 0 // the last rung lands exactly on zero
	}
	acc.quota.CPUMillis = next
	return acc.quota, nil
}

// FreezeMemoryPages freezes the agent's simulated memory pages. It runs AFTER
// the CPU walk-down, never before: the design order is CPU rungs first, then
// memory freeze.
func (b *InMemoryBackend) FreezeMemoryPages(agentID domain.AgentID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	if !ok {
		return fmt.Errorf("resource: agent %q has no allocated quota", agentID)
	}
	acc.quota.PagesFrozen = true
	return nil
}

// CutNetwork disconnects the agent's simulated network — the last reclaim
// step.
func (b *InMemoryBackend) CutNetwork(agentID domain.AgentID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	acc, ok := b.accounts[agentID]
	if !ok {
		return fmt.Errorf("resource: agent %q has no allocated quota", agentID)
	}
	acc.quota.NetworkOpen = false
	return nil
}
