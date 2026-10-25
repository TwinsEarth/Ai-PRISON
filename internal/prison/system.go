// Package prison is the v0.9.5 end-to-end orchestration glue. It wires the
// already-built, independently-tested layers — governance, revocation, audit,
// resource deprivation, isolation, identity & communication, migration
// suppression and capability blockade — into one in-memory System and drives a
// full defensive governance journey: detect → seven-stage two-phase consent →
// sentence → layered enforcement → audit verification → appeal / rehabilitation →
// (conditional) restoration.
//
// This package contains NO new control behaviour of its own: every state change
// is delegated to the real layer APIs, and cross-layer wiring lives here (adapters
// / glue) so that the individual packages keep their tested public contracts.
//
// # SIMULATION SCOPE
//
// Everything is an in-memory simulation. There is no real isolation, no real
// revocation distribution, no real hardware binding, no real capability removal
// and no real legal effect. The demo and the e2e tests therefore print / assert
// on simulated facts only (receipt states, CPU rung history, sandbox backend
// labels, hashes). This package is strictly defensive: isolation, auditability
// and human accountability — no escape, attack, weaponization or dangerous
// knowledge.
package prison

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/capability"
	"github.com/ai-prison/ai-prison/internal/enforcement/identity"
	"github.com/ai-prison/ai-prison/internal/enforcement/isolation"
	"github.com/ai-prison/ai-prison/internal/enforcement/migration"
	"github.com/ai-prison/ai-prison/internal/enforcement/resource"
	"github.com/ai-prison/ai-prison/internal/governance"
	"github.com/ai-prison/ai-prison/internal/revocation"
	"github.com/ai-prison/ai-prison/internal/store"
	"github.com/ai-prison/ai-prison/pkg/contracts"
)

// DefaultClock is the fixed, deterministic clock used across the orchestration
// so demo output is stable run-to-run. It does not hide the fact that every
// timestamp ultimately models a wall clock in this one process.
var DefaultClock = func() time.Time {
	return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
}

// custodianSet is the fixed simulated multi-party custody roster for L5.
var custodianSet = []string{"court", "escrow", "trustee"}

// custodyThreshold is M in the simulated M-of-N snapshot custody (N=3).
const custodyThreshold = 2

// monitorFreezeThreshold is the cumulative signal severity at which the
// behaviour monitor freezes an agent mid-anomaly.
const monitorFreezeThreshold = 10

// isoGate adapts the isolation Controller to the capability package's
// IsolationGate: a blockade may run only when the agent already lives inside a
// confirmed-ready sandbox (docs §3.1 — no escape window during the blockade).
type isoGate struct{ ctrl *isolation.Controller }

func (g isoGate) IsReady(a domain.AgentID) (bool, string) {
	live := g.ctrl.Live(a)
	if live == nil {
		return false, "no-live-sandbox"
	}
	return live.Ready, string(live.Backend)
}

// chainSink adapts *audit.Chain to the structural EventSink interfaces declared
// by the isolation / migration / capability / governance packages.
type chainSink struct{ chain *audit.Chain }

func (s chainSink) Append(ev domain.Event) domain.Event { return s.chain.Append(ev) }

// System is the fully wired, in-memory assembly of every AI Prison layer. It is
// the single entry point the CLI demo and the e2e tests drive.
type System struct {
	// Authoritative shared substrate — every layer talks to the SAME chain,
	// receipt book and revocation set.
	Chain *audit.Chain
	Book  *audit.ReceiptBook
	Set   *revocation.Set

	// Enforcement layers (real in-memory implementations).
	Resource   *resource.Engine
	ResBackend *resource.InMemoryBackend
	ResSnaps   *resource.MemSnapshotStore

	Gradient *isolation.Gradient
	Iso      *isolation.Controller
	Monitor  *isolation.Monitor

	Identity *identity.Service
	Topology *identity.InMemoryTopology

	Migration  *migration.Service
	Capability *capability.Service

	Gov *governance.Governance

	// Fixed clock + runtime bookkeeping owned by the orchestrator.
	clock func() time.Time
	// freezeHooks records, per agent, the total observed-signal count at the
	// moment the monitor froze it (mid-anomaly freeze evidence).
	freezeHooks map[domain.AgentID]int
	// runtimeStatus mirrors the simulated runtime lifecycle of an agent after
	// enforcement / restoration (the layer packages do not all expose a generic
	// status setter; the orchestrator owns this read-side projection).
	runtimeStatus map[domain.AgentID]domain.AgentStatus

	// --- durable-ledger wiring (v1.1); all nil/empty when Store == nil --------
	// recorder is nil in the default in-memory mode: nothing below compiles in
	// any write path, and behaviour/output is identical to v1.0.
	recorder *ledgerRecorder
	// restored is true once an existing ledger was replayed during NewSystem.
	restored bool
	// agentReputation mirrors the per-agent reputation score used in the
	// runtime-status projection.
	agentReputation map[domain.AgentID]int
	// proj is the read-side agent projection: maintained live when running,
	// rebuilt from the ledger on restart. InspectView reads it.
	proj map[domain.AgentID]*RestoredAgent
	// caseCount counts adjudicated cases (incremented live; rebuilt on replay).
	caseCount int
	// sentenceCount counts issued sentences (incremented live; rebuilt on replay).
	sentenceCount int
	// BootstrapErr sticks a replay failure (an open FileStore already verified
	// the record chain; a non-nil value here means a payload-decode bug).
	BootstrapErr error
}

// Config builds a System; zero value means "sensible deterministic in-memory
// defaults", which is the only configuration this prototype supports.
type Config struct {
	// Clock overrides the fixed clock; nil uses DefaultClock.
	Clock func() time.Time
	// Store, when non-nil, enables real crash-safe persistence of the
	// governance/audit record (see persist.go). nil keeps the v1.0 pure-memory
	// behaviour, output and tests unchanged.
	Store store.Store
}

// NewSystem assembles every layer with the real in-memory implementations and
// the real adapters, so all events land on one audit.Chain and all revocations
// land on one revocation.Set.
func NewSystem(cfg Config) *System {
	clock := cfg.Clock
	if clock == nil {
		clock = DefaultClock
	}

	chain := audit.NewChain()
	book := audit.NewReceiptBook()
	set := revocation.NewSet()

	// When a ledger is wired, every layer receives a recording sink that mirrors
	// stamped chain events onto the ledger; otherwise the v1.0 plain adapters
	// are used byte-for-byte unchanged.
	var rec *ledgerRecorder
	if cfg.Store != nil {
		rec = newRecorder(cfg.Store)
	}
	sp := recordingSink{chain: chain, rec: rec, clock: clock}

	// Resource layer.
	resSnaps := resource.NewMemSnapshotStore()
	resBackend := resource.NewInMemoryBackend(set)
	resEvents := resource.EventSink(resource.NewChainSink(chain))
	if rec != nil {
		resEvents = sp
	}
	resEngine := resource.NewEngine(resource.Config{
		Backend:     resBackend,
		Revocations: resource.NewRevocationSetSink(set),
		Events:      resEvents,
		Receipts:    resource.NewReceiptBookSink(book),
		Snapshots:   resSnaps,
	})
	resEngine.Now = clock

	// Isolation layer.
	gradient := isolation.NewGradient()
	isoSink := isolation.EventSink(chainSink{chain: chain})
	if rec != nil {
		isoSink = sp
	}
	isoCtrl := isolation.NewController(gradient, isoSink)

	// Identity & communication layer.
	topo := identity.NewInMemoryTopology()
	idEvents := identity.EventSink(identity.NewEventSink(chain))
	if rec != nil {
		idEvents = sp
	}
	idSvc := identity.NewService(identity.Config{
		Topology:    topo,
		Revocations: identity.NewRevocationSink(set),
		Events:      idEvents,
		Receipts:    identity.NewReceiptController(book),
	})

	// Migration-suppression layer (shared in-memory subsystems, inspected later).
	migEvents := migration.EventSink(migration.NewChainSink(chain))
	if rec != nil {
		migEvents = sp
	}
	migCustodySink := migration.EventSink(chainSink{chain: chain})
	if rec != nil {
		migCustodySink = sp
	}
	migSvc := migration.NewService(migration.Config{
		Binder:  migration.NewMemHardwareBinder(),
		Records: migration.NewMemRecordKeeper(),
		Custody: migration.NewMemCustodyStore(migCustodySink),
		Events:  migEvents,
	})

	// Capability-blockade layer, gated on the real isolation controller.
	capEvents := capability.EventSink(capability.NewChainSink(chain))
	if rec != nil {
		capEvents = sp
	}
	capSvc := capability.NewService(capability.Config{
		Gate:   isoGate{ctrl: isoCtrl},
		Events: capEvents,
	})

	// Governance layer. Principals are registered explicitly via Gov.RegisterAgent.
	govEvents := governance.EventSink(governance.NewEventSink(chain))
	if rec != nil {
		govEvents = sp
	}
	govSvc := governance.New(governance.Config{
		Events: govEvents,
		Now:    clock,
	})

	sys := &System{
		Chain:           chain,
		Book:            book,
		Set:             set,
		Resource:        resEngine,
		ResBackend:      resBackend,
		ResSnaps:        resSnaps,
		Gradient:        gradient,
		Iso:             isoCtrl,
		Identity:        idSvc,
		Topology:        topo,
		Migration:       migSvc,
		Capability:      capSvc,
		Gov:             govSvc,
		clock:           clock,
		freezeHooks:     make(map[domain.AgentID]int),
		runtimeStatus:   make(map[domain.AgentID]domain.AgentStatus),
		recorder:        rec,
		agentReputation: make(map[domain.AgentID]int),
		proj:            make(map[domain.AgentID]*RestoredAgent),
	}
	// Behaviour monitor: freeze mid-anomaly, record the ordinal it fired at.
	monSink := isolation.EventSink(chainSink{chain: chain})
	if rec != nil {
		monSink = sp
	}
	sys.Monitor = isolation.NewMonitor(monitorFreezeThreshold,
		func(agent domain.AgentID, _ isolation.Signal) {
			sys.freezeHooks[agent] = sys.Monitor.TotalObserved()
		},
		monSink)

	// Event-sourced boot: rebuild substrates + projection from an existing ledger.
	if rec != nil {
		if err := sys.replay(); err != nil {
			sys.BootstrapErr = err
		} else {
			sys.restored = true
		}
	}
	return sys
}

// Clock exposes the fixed clock for callers that need deterministic timestamps.
func (s *System) Clock() time.Time { return s.clock().UTC() }

// RuntimeStatus returns the orchestrator's simulated runtime status projection.
func (s *System) RuntimeStatus(a domain.AgentID) domain.AgentStatus {
	return s.runtimeStatus[a]
}

// resourceCredID is the deterministic resource-credential handle per agent.
func resourceCredID(a domain.AgentID) string { return "cred:resource:" + string(a) }

// capCredID is the deterministic capability-credential handle per agent
// (hardware-bound by migration suppression at L3+).
func capCredID(a domain.AgentID) string { return "cred:capability:" + string(a) }

// baseQuota is the simulated pre-sentence quota the agent runs on.
func baseQuota(a domain.AgentID) resource.Quota {
	return resource.Quota{
		AgentID:      a,
		CredentialID: resourceCredID(a),
		CPUMillis:    1000,
		MemoryBytes:  4 << 30,
		StorageBytes: 10 << 30,
		NetworkOpen:  true,
	}
}

// compile-time assurance the shared substrate satisfies the read-side contract.
var _ domain.RevocationChecker = (*revocation.Set)(nil)

// ensure contracts.ComplianceReport is the report type surfaced on a Journey.
var _ = contracts.ComplianceReport{}
