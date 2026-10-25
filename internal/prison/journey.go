package prison

import (
	"fmt"
	"strings"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/identity"
	"github.com/ai-prison/ai-prison/internal/enforcement/migration"
	"github.com/ai-prison/ai-prison/internal/governance"
	"github.com/ai-prison/ai-prison/pkg/contracts"
)

// Step is one narrated row of a Journey, in execution order. It exists so the
// CLI can print a segmented, human-readable trace and tests can grep it.
type Step struct {
	Stage  string
	Detail string
}

// Journey is the structured, per-scenario trace of a full governance flow. Every
// field captures one key fact the demo prints and the e2e tests assert on.
type Journey struct {
	// Per-scenario knobs set by the caller before Enforce runs.
	// SilentNode is the node that deliberately never acknowledges the channel
	// cut (demonstrates untrusted-node + collateral freeze); "" = all ack.
	SilentNode  string
	Fingerprint string
	// MonitorDemo feeds the behaviour monitor a short signal stream so the
	// mid-anomaly freeze is observable; ProbeCapBlock attempts a blockade
	// BEFORE any sandbox exists to prove the isolation gate refuses it.
	MonitorDemo   bool
	ProbeCapBlock bool
	// RecordKey is the simulated portable-work-record anchor key issued at seed.
	RecordKey string

	Scenario string

	// Governance facts.
	Violation domain.Violation
	CaseID    string
	Sentence  *domain.Sentence

	// Identity & communication facts.
	CardRevoked        bool
	RevokedCredentials []string
	HostingNodes       []string
	UnackedNode        string                 // node that never acknowledged the channel cut
	UntrustedNodes     []string               // nodes the receipt book judges untrusted
	FrozenAgents       []identity.FreezeOrder // incl. collateral (group-freeze) agents

	// Resource-deprivation facts.
	CPUHistory    []int  // 1000 -> ... -> 0, multiple intermediate rungs (non-instant)
	SnapshotHash  string // pre-reclaim snapshot content hash, anchored on the chain
	SnapshotAtCPU int    // CPU quota at seal time (must be the live pre-reclaim 1000)

	// Isolation facts.
	LiveBackend     string // sandbox backend the agent finally lives in
	FreezeSignalAt  int    // 1-based signal ordinal at which the monitor froze mid-anomaly (0 = never)
	MonitorFroze    bool
	CapBlockRefused string // error when a blockade was attempted outside isolation

	// Migration-suppression facts.
	MigrationApplied []string
	// RecordKeyNonPortable is true when the portable-work-record anchor key was
	// revoked (L5): a migrated copy can no longer carry the agent's history.
	RecordKeyNonPortable bool
	SealedCustody        *migration.SealedCustody

	// Capability-blockade facts (L4 only; nil for the L3 / L5 scenarios).
	PostBlockHash string

	// Reputation / runtime projection.
	Reputation int

	// Appeal / restoration facts.
	AppealSuspended  bool
	AppealUpheld     bool
	RestorationOrder *governance.RestorationOrder
	Restored         bool

	// Final audit facts.
	Compliance       contracts.ComplianceReport
	ChainVerifyError error
	FinalStatus      domain.AgentStatus

	// Narrated steps, in order.
	Steps []Step
}

// Note appends one narrated step.
func (j *Journey) Note(stage, detail string) {
	j.Steps = append(j.Steps, Step{Stage: stage, Detail: detail})
}

// NewJourney starts an empty trace for one scenario.
func (sys *System) NewJourney(scenario string) *Journey {
	return &Journey{Scenario: scenario, Reputation: 800}
}

// InnocentCollateral returns the (group-frozen) agents frozen only because they
// were co-located on an untrusted node — not because they were sentenced.
func (j *Journey) InnocentCollateral() []domain.AgentID {
	var out []domain.AgentID
	for _, f := range j.FrozenAgents {
		if f.GroupFreeze {
			out = append(out, f.AgentID)
		}
	}
	return out
}

// short truncates a hash for compact printing.
func short(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

// print renders the Journey as segmented Chinese (key terms bilingual) text.
// It is used by the CLI demo; tests assert on structured fields instead.
func (j *Journey) Print() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("═══ 场景 %s ═══", j.Scenario)
	for _, s := range j.Steps {
		w("  [%s] %s", s.Stage, s.Detail)
	}
	w("── 关键事实汇总 ──")
	w("  等级 Level:        %s", j.levelCode())
	if j.Sentence != nil {
		w("  案件/判决:        %s / %s", j.CaseID, j.Sentence.ID)
	}
	w("  CPU 档位历史:     %v", j.CPUHistory)
	w("  回收前快照哈希:    %s (seal 时 CPU=%d)", short(j.SnapshotHash), j.SnapshotAtCPU)
	w("  沙箱后端:         %s", na(j.LiveBackend))
	if j.MonitorFroze {
		w("  监控冻结:         第 %d 个信号进行中即冻结(非事后)", j.FreezeSignalAt)
	}
	w("  未回执节点:       %s", na(strings.Join(j.UntrustedNodes, ",")))
	if len(j.FrozenAgents) > 0 {
		w("  连带冻结 Agent:   %v", agentIDs(j.FrozenAgents))
	}
	if j.CardRevoked {
		w("  AgentCard 作废:   true")
	}
	if len(j.RevokedCredentials) > 0 {
		w("  吊销凭证:         %s", strings.Join(j.RevokedCredentials, ", "))
	}
	w("  迁移抑制措施:     %s", na(strings.Join(j.MigrationApplied, ", ")))
	if j.SealedCustody != nil {
		w("  快照多方托管:     M=%d/N=%d custodians=%s",
			j.SealedCustody.Threshold, j.SealedCustody.Total, strings.Join(j.SealedCustody.Custodians, ","))
	}
	if j.RecordKeyNonPortable {
		w("  记录可迁移性:     false(锚定密钥已吊销，迁移副本无法继承信誉/历史)")
	}
	if j.PostBlockHash != "" {
		w("  post-block hash:  %s", short(j.PostBlockHash))
	}
	if j.CapBlockRefused != "" {
		w("  能力封锁预检:     拒绝(隔离外不可摘除): %s", j.CapBlockRefused)
	}
	w("  剩余信誉:         %d", j.Reputation)
	w("  申诉暂停执行:     %v", j.AppealSuspended)
	if j.RestorationOrder != nil {
		w("  恢复令:           %s (状态 restored)", j.RestorationOrder.OrderID)
	} else if j.AppealUpheld {
		w("  恢复令:           无(L5 程序性受理，身份不解封)")
	}
	w("  审计链校验:       %s", verifyLabel(j.ChainVerifyError))
	w("  合规报告:         per-hop=%v e2e=%v pending=%d untrustedNodes=%d",
		j.Compliance.PerHopCompliant, j.Compliance.EndToEndCompliant,
		j.Compliance.PendingReceipts, j.Compliance.UntrustedNodes)
	if j.Compliance.Notes != "" {
		w("  合规备注:         %s", j.Compliance.Notes)
	}
	w("  最终运行状态:     %s", j.FinalStatus)
	return b.String()
}

func (j *Journey) levelCode() string {
	if j.Sentence == nil {
		return "—"
	}
	return j.Sentence.Level.Code()
}

func agentIDs(fs []identity.FreezeOrder) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f.AgentID)
	}
	return out
}

func na(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func verifyLabel(err error) string {
	if err == nil {
		return "OK"
	}
	return "FAIL: " + err.Error()
}
