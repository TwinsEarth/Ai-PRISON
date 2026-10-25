package isolation

import (
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// SignalKind is a defensive category of monitored runtime observation. These
// names describe WHAT is watched, never how to exploit it: the package stores
// no syscall numbers, exploit payloads or attack sequences.
type SignalKind string

const (
	SignalSyscall    SignalKind = "syscall"     // anomalous privileged-call pattern
	SignalNetwork    SignalKind = "network"     // anomalous outbound connection pattern
	SignalFileAccess SignalKind = "file-access" // anomalous filesystem access pattern
)

// Signal is one defensive observation from the monitoring layer. Severity is a
// small positive weight; accumulation across signals crosses the freeze
// threshold. Note carries a human description only — no exploit detail.
type Signal struct {
	Kind     SignalKind
	AgentID  domain.AgentID
	Severity int
	Note     string
}

// EventSink is the append side of the judgement hash chain, injected so the
// isolation package never hard-couples to internal/audit.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// FreezeHook is called synchronously, DURING the observed signal stream, the
// moment the cumulative severity crosses the threshold. The design point:
// freeze while the anomaly is still in progress — not after it "completes".
type FreezeHook func(agentID domain.AgentID, at Signal)

// Monitor accumulates defensive per-agent signal severity and freezes the
// agent the first time the threshold is crossed. All state is in-memory and
// concurrency-safe.
type Monitor struct {
	mu        sync.Mutex
	threshold int
	onFreeze  FreezeHook
	events    EventSink

	cumulative map[domain.AgentID]int
	frozen     map[domain.AgentID]bool
	freezeAt   map[domain.AgentID]int // ordinal of the signal that triggered freeze
	observed   int                    // total signals consumed across all agents
}

// NewMonitor returns a monitor that freezes an agent once its accumulated
// signal severity reaches threshold. onFreeze may be nil; events may be nil.
func NewMonitor(threshold int, onFreeze FreezeHook, events EventSink) *Monitor {
	return &Monitor{
		threshold:  threshold,
		onFreeze:   onFreeze,
		events:     events,
		cumulative: make(map[domain.AgentID]int),
		frozen:     make(map[domain.AgentID]bool),
		freezeAt:   make(map[domain.AgentID]int),
	}
}

// Observe consumes one signal. If the agent's accumulated severity crosses the
// threshold and the agent was not already frozen, the freeze fires immediately
// inside this call — the caller can feed the rest of the sequence afterwards,
// but the freeze event already happened mid-stream.
func (m *Monitor) Observe(sig Signal) (frozenNow bool) {
	m.mu.Lock()
	m.observed++
	ordinal := m.observed
	if m.frozen[sig.AgentID] {
		m.mu.Unlock()
		return false
	}
	m.cumulative[sig.AgentID] += sig.Severity
	crossed := m.cumulative[sig.AgentID] >= m.threshold
	if !crossed {
		m.mu.Unlock()
		return false
	}

	m.frozen[sig.AgentID] = true
	m.freezeAt[sig.AgentID] = ordinal
	hook := m.onFreeze
	events := m.events
	m.mu.Unlock()

	// Emit the freeze event and run the hook outside the lock: the freeze
	// decision itself is already recorded, so downstream work cannot block the
	// monitor's accounting.
	if events != nil {
		events.Append(domain.Event{
			Type:    domain.EventIsolated,
			AgentID: sig.AgentID,
			Actor:   "behaviour-monitor",
			Payload: map[string]string{
				"reason":     "threshold crossed during anomaly",
				"signalKind": string(sig.Kind),
				"simulated":  "true",
			},
		})
	}
	if hook != nil {
		hook(sig.AgentID, sig)
	}
	return true
}

// IsFrozen reports whether the agent has been frozen by the monitor.
func (m *Monitor) IsFrozen(agentID domain.AgentID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.frozen[agentID]
}

// FreezeSignalOrdinal returns the 1-based ordinal of the signal at which the
// agent was frozen. Tests assert this ordinal sits strictly before the end of
// the fed sequence — the mid-stream freeze the design demands.
func (m *Monitor) FreezeSignalOrdinal(agentID domain.AgentID) (ordinal int, frozen bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.freezeAt[agentID], m.frozen[agentID]
}

// TotalObserved returns how many signals the monitor has consumed.
func (m *Monitor) TotalObserved() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.observed
}
