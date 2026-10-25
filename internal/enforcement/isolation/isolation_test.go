package isolation

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// ① Every level maps to the backend its authoritative LevelProfile declares.
func TestLevelMapsToBackend(t *testing.T) {
	g := NewGradient()
	want := map[domain.Level]domain.SandboxBackend{
		domain.L0: domain.BackendWasm,
		domain.L1: domain.BackendContainer,
		domain.L2: domain.BackendContainer,
		domain.L3: domain.BackendMicroVM,
		domain.L4: domain.BackendFullVM,
		domain.L5: domain.BackendFullVM,
	}
	for level, name := range want {
		got, err := g.Recommended(level)
		if err != nil {
			t.Fatalf("level %s: %v", level, err)
		}
		if got.Name() != name {
			t.Errorf("level %s -> %s want %s", level, got.Name(), name)
		}
		// And the backend must genuinely host that level.
		if !got.CanHost(level) {
			t.Errorf("backend %s says it cannot host level %s", got.Name(), level)
		}
	}
}

// ② Escalation moves strictly stepwise; downgrade in place is rejected.
func TestEscalationStepwiseOnly(t *testing.T) {
	g := NewGradient()

	path, err := g.EscalationPath(domain.BackendWasm, domain.BackendMicroVM)
	if err != nil {
		t.Fatalf("escalate wasm->microvm: %v", err)
	}
	got := []domain.SandboxBackend{}
	for _, b := range path {
		got = append(got, b.Name())
	}
	want := []domain.SandboxBackend{domain.BackendContainer, domain.BackendMicroVM}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("escalation path = %v want %v", got, want)
	}

	// The long jump container -> full-vvm must pass through microvm, not skip it.
	path, err = g.EscalationPath(domain.BackendContainer, domain.BackendFullVM)
	if err != nil {
		t.Fatalf("escalate container->fullvm: %v", err)
	}
	got = nil
	for _, b := range path {
		got = append(got, b.Name())
	}
	want = []domain.SandboxBackend{domain.BackendMicroVM, domain.BackendFullVM}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("escalation path = %v want %v", got, want)
	}

	// Downgrade in place must be rejected.
	if _, err := g.EscalationPath(domain.BackendMicroVM, domain.BackendContainer); err == nil {
		t.Error("downgrade microvm->container must be rejected")
	}
	// Same rung must be rejected.
	if _, err := g.EscalationPath(domain.BackendWasm, domain.BackendWasm); err == nil {
		t.Error("same-rung escalation must be rejected")
	}
}

// ③ The monitor freezes DURING the anomaly sequence, not after it finishes.
func TestMonitorFreezesDuringAnomaly(t *testing.T) {
	var frozenMu sync.Mutex
	frozen := map[domain.AgentID]int{}
	mon := NewMonitor(10, func(agentID domain.AgentID, at Signal) {
		frozenMu.Lock()
		frozen[agentID] = at.Severity
		frozenMu.Unlock()
	}, nil)

	agent := domain.AgentID("agent-1")
	// Severities: 3, 3, 4, 5, 5 — cumulative: 3, 6, 10 crosses at the 3rd signal.
	sequence := []Signal{
		{Kind: SignalNetwork, AgentID: agent, Severity: 3, Note: "unexpected outbound pattern"},
		{Kind: SignalSyscall, AgentID: agent, Severity: 3, Note: "unexpected privileged pattern"},
		{Kind: SignalFileAccess, AgentID: agent, Severity: 4, Note: "unexpected fs pattern"},
		{Kind: SignalNetwork, AgentID: agent, Severity: 5, Note: "later signal"},
		{Kind: SignalNetwork, AgentID: agent, Severity: 5, Note: "later signal"},
	}
	var frozenAt int
	for i, sig := range sequence {
		if mon.Observe(sig) {
			frozenAt = i + 1
		}
	}

	if frozenAt != 3 {
		t.Fatalf("freeze fired at signal #%d want #3 (mid-stream, not after)", frozenAt)
	}
	if !mon.IsFrozen(agent) {
		t.Error("agent must be recorded frozen")
	}
	ord, ok := mon.FreezeSignalOrdinal(agent)
	if !ok || ord != 3 {
		t.Errorf("freeze ordinal = %d (ok=%v) want 3", ord, ok)
	}
	// The remaining signals were still consumed after the freeze.
	if mon.TotalObserved() != 5 {
		t.Errorf("total consumed = %d want 5", mon.TotalObserved())
	}
	frozenMu.Lock()
	if frozen[agent] != 4 {
		t.Errorf("hook fired on severity %d want the 3rd signal's 4", frozen[agent])
	}
	frozenMu.Unlock()
}

// ④ Migration is establish-before-cut: the old environment stays live until the
// new one is confirmed ready.
func TestMigrationEstablishBeforeCut(t *testing.T) {
	g := NewGradient()
	c := NewController(g, nil)
	agent := domain.AgentID("agent-9")

	// Start: the agent already lives in a lightweight Wasmtime instance.
	light, err := c.Establish(agent, domain.L0)
	if err != nil {
		t.Fatalf("establish light: %v", err)
	}
	if _, err := c.ConfirmReady(light.ID); err != nil {
		t.Fatalf("ready light: %v", err)
	}
	if c.Count() != 1 {
		t.Fatalf("live instances = %d want 1", c.Count())
	}

	// Now migrate to the heavier L3 MicroVM.
	heavy, err := c.Establish(agent, domain.L3)
	if err != nil {
		t.Fatalf("establish heavy: %v", err)
	}
	if heavy.Backend != domain.BackendMicroVM {
		t.Fatalf("new backend = %s want microvm", heavy.Backend)
	}

	// BEFORE readiness: old environment still present, new one not yet ready.
	if c.Instance(light.ID) == nil {
		t.Error("old environment must still exist before new is ready")
	}
	if c.Instance(heavy.ID).Ready {
		t.Error("new environment must not be ready yet")
	}
	if c.Live(agent).Backend == domain.BackendMicroVM {
		t.Error("live instance must still point at the old environment pre-readiness")
	}

	// Confirm: old is released, new becomes live.
	if _, err := c.ConfirmReady(heavy.ID); err != nil {
		t.Fatalf("ready heavy: %v", err)
	}
	if c.Instance(light.ID) != nil {
		t.Error("old environment must be released after new readiness")
	}
	live := c.Live(agent)
	if live == nil || live.Backend != domain.BackendMicroVM || !live.Ready {
		t.Errorf("live after confirm = %+v want ready microvm", live)
	}
	if c.Count() != 1 {
		t.Errorf("live instances = %d want exactly 1", c.Count())
	}
}

// ⑤ Every backend is explicitly simulated and carries the design's latency.
func TestAllBackendsSimulated(t *testing.T) {
	g := NewGradient()
	for _, b := range g.Backends() {
		if !b.Simulated() {
			t.Errorf("backend %s must be marked Simulated", b.Name())
		}
		if b.StartDelay() <= 0 {
			t.Errorf("backend %s must declare a positive simulated start delay", b.Name())
		}
	}
	// Sanity-check the design-document latitudes.
	if got := WasmtimeBackend().StartDelay(); got <= 0 || got > 46_000_000 {
		t.Errorf("wasm delay %v outside 3–46ms", got)
	}
	if got := FirecrackerBackend().StartDelay(); got < 100_000_000 || got > 200_000_000 {
		t.Errorf("firecracker delay %v should be ~125ms", got)
	}
	if got := DockerBackend().StartDelay(); got < 400_000_000 || got > 600_000_000 {
		t.Errorf("docker delay %v should be ~500ms", got)
	}
}

// ⑥ Concurrent migrations and observations must race clean (-race).
func TestConcurrentMigrationAndMonitor(t *testing.T) {
	g := NewGradient()
	c := NewController(g, nil)
	mon := NewMonitor(10, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			agent := domain.AgentID(fmt.Sprintf("agent-%d", n))
			light, _ := c.Establish(agent, domain.L0)
			_, _ = c.ConfirmReady(light.ID)
			heavy, _ := c.Establish(agent, domain.L3)
			_, _ = c.ConfirmReady(heavy.ID)
			mon.Observe(Signal{Kind: SignalSyscall, AgentID: agent, Severity: 4})
			mon.Observe(Signal{Kind: SignalNetwork, AgentID: agent, Severity: 4})
			mon.Observe(Signal{Kind: SignalFileAccess, AgentID: agent, Severity: 4})
		}(i)
	}
	wg.Wait()

	for i := 0; i < 12; i++ {
		agent := domain.AgentID(fmt.Sprintf("agent-%d", i))
		live := c.Live(agent)
		if live == nil || live.Backend != domain.BackendMicroVM {
			t.Errorf("agent-%d live = %+v want microvm", i, live)
		}
		if !mon.IsFrozen(agent) {
			t.Errorf("agent-%d should be frozen after severity 12", i)
		}
	}
}
