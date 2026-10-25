package resource

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// DefaultSteps is the default number of gradual CPU downgrade rungs. The CPU
// quota walks from 100% to 0 across this many rungs; it must never jump
// straight to zero in a single step.
const DefaultSteps = 4

// Quota is a *revocable* resource-allocation token for one agent, modelled as
// a credential rather than a static configuration. The CredentialID is the
// handle the global revocation set consults before any allocation.
//
// NOTE: this quota is fully simulated; it does not touch the kernel, cgroups
// or the network stack.
type Quota struct {
	AgentID      domain.AgentID `json:"agentId"`
	CredentialID string         `json:"credentialId"` // revocable resource credential
	CPUMillis    int            `json:"cpuMillis"`    // simulated CPU quota; walked down rung by rung
	MemoryBytes  int64          `json:"memoryBytes"`
	StorageBytes int64          `json:"storageBytes"`
	NetworkOpen  bool           `json:"networkOpen"`
	PagesFrozen  bool           `json:"pagesFrozen"`
}

// Zeroed reports whether the quota has been fully reclaimed: CPU runged down
// to zero, pages frozen and network cut.
func (q Quota) Zeroed() bool {
	return q.CPUMillis <= 0 && !q.NetworkOpen && q.PagesFrozen
}

// Backend grants and reclaims simulated compute resources. It is an
// interface on purpose: the shipped implementation is in-memory only, and
// tests can substitute fakes without pulling in real orchestration.
type Backend interface {
	// Allocate grants q. Implementations MUST consult the revocation set
	// first: a revoked credential is rejected even if the quota would
	// otherwise fit (the "revoked but not expired" cache-window rule).
	Allocate(q Quota) error
	// Current returns the live quota of an agent.
	Current(agentID domain.AgentID) (Quota, bool)
	// StopAllocation switches the agent into the "no new allocations" state
	// after the revocation set has been written.
	StopAllocation(agentID domain.AgentID)
	// AllocationStopped reports whether new allocations are frozen for the
	// agent.
	AllocationStopped(agentID domain.AgentID) bool
	// PlanReclaim records how many gradual rungs the reclaim loop will use.
	PlanReclaim(agentID domain.AgentID, steps int) error
	// ReclaimStep executes ONE gradual downgrade rung. It MUST lower the CPU
	// quota by exactly one rung and never jump straight to zero. Repeated
	// calls after the quota reached zero are idempotent no-ops.
	ReclaimStep(agentID domain.AgentID) (Quota, error)
	// FreezeMemoryPages freezes the agent's memory pages (after CPU has been
	// walked down). Simulated.
	FreezeMemoryPages(agentID domain.AgentID) error
	// CutNetwork disconnects the agent's network (the final reclaim step).
	// Simulated.
	CutNetwork(agentID domain.AgentID) error
}

// RevocationSink is the write side of the global revocation set, injected so
// the resource package never hard-couples to internal/revocation.
type RevocationSink interface {
	RevokeResource(id string, agentID domain.AgentID, sentenceID, reason string, holdingNodes []string)
}

// EventSink is the append side of the judgement hash chain, injected so the
// resource package never hard-couples to internal/audit.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// ReceiptSink is the broadcast side of the execution receipt book:
// "broadcast" is never treated as "delivered".
type ReceiptSink interface {
	RegisterPending(r domain.Receipt) domain.Receipt
}

// Snapshot is a simulated pack_diff evidence record of an agent's environment
// taken BEFORE reclamation. ContentHash anchors the tamper-evident evidence;
// in production this would be multi-party encrypted custody (v0.8).
type Snapshot struct {
	ID          string         `json:"id"`
	AgentID     domain.AgentID `json:"agentId"`
	SentenceID  string         `json:"sentenceId"`
	ContentHash string         `json:"contentHash"`
	SealedAt    time.Time      `json:"sealedAt"`
	Quota       Quota          `json:"quotaAtSeal"`
}

// SnapshotStore seals simulated snapshots into isolated (in-memory) storage.
type SnapshotStore interface {
	Seal(s Snapshot) (Snapshot, error)
	Get(id string) (Snapshot, bool)
}
