package isolation

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Backend abstracts one isolation backend on the strength continuum. Every
// implementation in this package is simulated: no real container, MicroVM or
// hardware isolation exists — see the package doc.
type Backend interface {
	// Name is the canonical label, aligned with domain.SandboxBackend.
	Name() domain.SandboxBackend
	// StartDelay is the simulated cold-start latency, matching the design
	// document's figures (Wasmtime 3–46ms, Docker ~500ms, Firecracker ~125ms,
	// Full VM seconds).
	StartDelay() time.Duration
	// Mechanism is the one-line description of the isolation mechanism.
	Mechanism() string
	// Simulated always returns true here, so callers can never mistake this
	// for a real control.
	Simulated() bool
	// CanHost reports whether this backend may hold an agent at level.
	CanHost(level domain.Level) bool
}

// simulatedBackend is the shared in-memory backend record.
type simulatedBackend struct {
	name      domain.SandboxBackend
	delay     time.Duration
	mechanism string
	hosts     map[domain.Level]bool
}

func (b simulatedBackend) Name() domain.SandboxBackend { return b.name }
func (b simulatedBackend) StartDelay() time.Duration   { return b.delay }
func (b simulatedBackend) Mechanism() string           { return b.mechanism }
func (b simulatedBackend) Simulated() bool             { return true }
func (b simulatedBackend) CanHost(level domain.Level) bool {
	return b.hosts[level]
}

// WasmtimeBackend is the lightweight daily-monitoring sandbox: linear-memory
// bounds checks, language-level isolation, 3–46ms startup. Holds L0–L1.
// Simulated, not a real Wasmtime runtime.
func WasmtimeBackend() Backend {
	return simulatedBackend{
		name:      domain.BackendWasm,
		delay:     25 * time.Millisecond, // within the design's 3–46ms range
		mechanism: "linear-memory bounds check, language-level isolation (SIMULATED: no Wasmtime runtime)",
		hosts:     map[domain.Level]bool{domain.L0: true, domain.L1: true},
	}
}

// DockerBackend is the restricted-execution sandbox: namespace + cgroup,
// ~500ms startup. Holds L1–L2. Simulated, not a real container runtime.
func DockerBackend() Backend {
	return simulatedBackend{
		name:      domain.BackendContainer,
		delay:     500 * time.Millisecond,
		mechanism: "namespace + cgroup process isolation (SIMULATED: no Docker runtime)",
		hosts:     map[domain.Level]bool{domain.L1: true, domain.L2: true},
	}
}

// FirecrackerBackend is the high-security MicroVM: KVM hardware isolation
// with an independent guest kernel, ~125ms startup. Holds L3.
// Simulated, not a real Firecracker / KVM instance.
func FirecrackerBackend() Backend {
	return simulatedBackend{
		name:      domain.BackendMicroVM,
		delay:     125 * time.Millisecond,
		mechanism: "KVM hardware isolation, independent guest kernel (SIMULATED: no Firecracker/MicroVM)",
		hosts:     map[domain.Level]bool{domain.L3: true},
	}
}

// FullVMBackend is the highest-containment sandbox: a complete guest OS,
// second-scale startup. Holds L3–L5 (L3–L4 high-risk, L5 read-only audit
// only). Simulated, not a real virtual machine.
func FullVMBackend() Backend {
	return simulatedBackend{
		name:      domain.BackendFullVM,
		delay:     2 * time.Second,
		mechanism: "full guest-OS isolation (SIMULATED: no real VM)",
		hosts:     map[domain.Level]bool{domain.L3: true, domain.L4: true, domain.L5: true},
	}
}

// registeredBackends is the ordered strength continuum, lightest to heaviest.
var registeredBackends = []Backend{
	WasmtimeBackend(),
	DockerBackend(),
	FirecrackerBackend(),
	FullVMBackend(),
}
