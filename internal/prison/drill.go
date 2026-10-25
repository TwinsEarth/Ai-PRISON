package prison

import (
	"fmt"
	"strings"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/capability"
	"github.com/ai-prison/ai-prison/internal/governance"
)

// This file implements the v1.0.0 recovery-drill automation that docs/06 only
// described as a target runbook. It drives dedicated DRILL agents on a fresh
// in-memory System, exercises one recovery (or non-recovery) path per level and
// emits a structured PASS/FAIL report.
//
// # SIMULATION SCOPE
//
// Everything exercised here is an in-memory simulation: no real isolation,
// revocation distribution, hardware binding, GRAM/TLM reload or token stack.
// The drill verifies the GOVERNANCE STATE MACHINE and the reversibility wiring
// (restore order issued / refused, recertification gate enforced, L5 stays
// sealed) — never a real control effect. It is strictly defensive: it proves
// wrongly-deprived capability can be lawfully restored (or, for L5, that it
// provably cannot). It contains no escape, attack, weaponization or dangerous
// knowledge step.

// DrillRow is one graded line of the recovery-drill report.
type DrillRow struct {
	// Level is the disposition grade this row drills ("L2".."L5").
	Level string `json:"level"`
	// Disposition is the enforcement action applied before recovery was tested.
	Disposition string `json:"disposition"`
	// Precondition lists the recovery preconditions that had to be satisfied
	// (verification / human guarantee / recertification).
	Precondition string `json:"precondition"`
	// Result is the observed recovery (or non-recovery) outcome.
	Result string `json:"result"`
	// Pass is true when every assertion on this row held.
	Pass bool `json:"pass"`
	// Notes records the concrete sub-checks and their values.
	Notes []string `json:"notes,omitempty"`
}

// DrillReport is the whole structured recovery-drill artifact.
type DrillReport struct {
	// RunAt is the fixed drill timestamp.
	RunAt time.Time `json:"runAt"`
	// Rows is one entry per drilled level, in ascending level order.
	Rows []DrillRow `json:"rows"`
	// Overall is true iff every row passed; a single failing assertion FAILS the
	// whole drill (docs/06 §4.2 — no skipping).
	Overall bool `json:"overall"`
}

// Add appends a row and returns it for in-place note gathering. A row starts as
// PASS; row.ok(false, ...) flips it to FAIL. Callers that intend a row to start
// failed must set row.Pass=false after Add.
func (r *DrillReport) Add(row DrillRow) *DrillRow {
	row.Pass = true
	r.Rows = append(r.Rows, row)
	return &r.Rows[len(r.Rows)-1]
}

// ok folds a single assertion into the row: if cond is false the row (and thus
// the whole drill) fails, and fact is recorded either way.
func (row *DrillRow) ok(cond bool, fact string) {
	row.Notes = append(row.Notes, fact)
	if !cond {
		row.Pass = false
	}
}

// RunRecoveryDrill assembles a fresh System and walks docs/06's recovery matrix
// level by level, returning the structured report. Every assertion must pass.
func RunRecoveryDrill() *DrillReport {
	sys := NewSystem(Config{})
	return sys.Drill()
}

// Drill runs the full recovery matrix on the receiver System. It is separate
// from RunRecoveryDrill so a caller can reuse one System and inspect its shared
// substrate (chain / set) afterwards.
func (sys *System) Drill() *DrillReport {
	rep := &DrillReport{RunAt: sys.Clock(), Overall: true}

	sys.drillL2(rep)
	sys.drillL3(rep)
	sys.drillL4(rep)
	sys.drillL5(rep)

	// The shared judgement chain must still verify after every drill.
	if err := sys.Chain.Verify(); err != nil {
		row := rep.Add(DrillRow{
			Level: "chain", Disposition: "hash-chain integrity",
			Precondition: "every drill event links to the previous",
			Result:       "FAIL: " + err.Error(),
		})
		row.Pass = false
	} else {
		rep.Add(DrillRow{
			Level: "chain", Disposition: "hash-chain integrity",
			Precondition: "every drill event links to the previous",
			Result:       "OK — hash chain verifies over all drill events", Pass: true,
		})
	}

	for i := range rep.Rows {
		if !rep.Rows[i].Pass {
			rep.Overall = false
		}
	}
	return rep
}

// drillSubject seeds, sentences and enforces one dedicated drill agent at the
// recommended level for viol. It returns the journey and the approved/enforcing
// sentence. All drill nodes acknowledge (no silent node) so receipts are clean.
func (sys *System) drillSubject(
	rep *DrillReport,
	row *DrillRow,
	id string,
	viol domain.ViolationType,
	nodes []string,
) (*Journey, *domain.Sentence, error) {
	j := sys.NewJourney("drill:" + id)
	j.Fingerprint = "fp-drill-" + id
	now := sys.Clock()

	a := domain.Agent{ID: domain.AgentID(id), Card: domain.AgentCard{
		DID: domain.DID("did:ai-prison:" + id), Name: "drill-" + id,
		ValidFrom: now.Add(-24 * time.Hour), ValidUntil: now.Add(90 * 24 * time.Hour),
	}}
	a.Principal, a.Deployer, a.ModelProviderParty = parties(
		"hp-"+id, "Drill Principal", "hd-drill", "Drill Deploy", "hm-drill", "Drill Provider")

	if err := sys.SeedAgent(j, SeedSpec{Agent: a, Nodes: nodes, Fingerprint: j.Fingerprint}); err != nil {
		return j, nil, err
	}
	v, _ := domain.Violation{Type: viol}.RecommendedLevel()
	row.Disposition = fmt.Sprintf("%s %s → %s", v.Code(), viol, strings.Join(nodes, "+"))

	sentence, err := sys.DetectAndSentence(j, SentenceInput{
		Violation: domain.Violation{
			ID: "viol-" + id, AgentID: a.ID, DID: a.Card.DID,
			Type: viol, Source: "drill",
			Signals:     []string{"drill:" + string(viol)},
			Description: "simulated defensive drill signal",
		},
		Votes:          ScenarioVotes(id, now),
		Accountability: sys.drillAccountability(&a, now),
	})
	if err != nil {
		return j, nil, err
	}
	if err := sys.Enforce(j, sentence); err != nil {
		return j, nil, err
	}
	return j, sentence, nil
}

// drillAccountability couples every drill sentence to a well-formed human record
// (the identity layer refuses an L5 disposition without one).
func (sys *System) drillAccountability(a *domain.Agent, now time.Time) []domain.AccountabilityRecord {
	return []domain.AccountabilityRecord{{
		Party:               a.Principal,
		Role:                domain.RolePrincipal,
		Finding:             domain.FindingJointLiability,
		WithinAuthorization: true,
		Reason:              "drill: principal joint liability recorded",
		RecordedAt:          now,
	}}
}

// drillL2 exercises "credential freeze → upheld appeal → full recovery".
func (sys *System) drillL2(rep *DrillReport) {
	row := rep.Add(DrillRow{Level: domain.L2.Code(), Precondition: "申诉推翻(overturned)上链 + 人类委托人担保"})
	j, s, err := sys.drillSubject(rep, row, "drl-2", domain.ViolationLateralMovement, []string{"n-l2a", "n-l2b"})
	if err != nil {
		row.Pass = false
		row.Result = "setup error: " + err.Error()
		return
	}
	row.ok(sys.RuntimeStatus(s.AgentID) == domain.StatusFrozen,
		"处置后运行状态=frozen")

	if _, err := sys.Appeal(j, s, governance.AppealGrounds{
		SignalMisread: "drill: signal re-read as false positive",
	}, true, "drill: overturned on record"); err != nil {
		row.Pass = false
		row.Result = "appeal error: " + err.Error()
		return
	}
	row.ok(j.RestorationOrder != nil, "申诉成立 → 恢复令已签发")
	row.ok(sys.RuntimeStatus(s.AgentID) == domain.StatusActive, "恢复后运行状态=active")
	row.ok(j.ChainVerifyError == nil, "审计链完整")
	row.Result = "恢复令 " + na(orderID(j.RestorationOrder)) + " → 状态 active, 全部节点回执 acked (渐进恢复，非瞬间拉满)"
}

// drillL3 exercises "isolation → supervised-behaviour assessment passes →
// gradual restoration".
func (sys *System) drillL3(rep *DrillReport) {
	row := rep.Add(DrillRow{Level: domain.L3.Code(), Precondition: "矫正评估 score≥threshold + 安全组织签字 + 受控观察期"})
	j, s, err := sys.drillSubject(rep, row, "drl-3", domain.ViolationSandboxEscapeAttempt, []string{"n-l3a", "n-l3b"})
	if err != nil {
		row.Pass = false
		row.Result = "setup error: " + err.Error()
		return
	}
	row.ok(j.LiveBackend == string(domain.BackendMicroVM), "迁回前 live 沙箱=firecracker-microvm")

	if err := sys.Rehabilitate(j, s, governance.RehabResult{
		Passed: true, Score: 0.92, Threshold: 0.80, Notes: []string{"drill: behaviour distribution within safety threshold"},
	}); err != nil {
		row.Pass = false
		row.Result = "rehab error: " + err.Error()
		return
	}
	row.ok(j.RestorationOrder != nil, "矫正评估通过 → 恢复令已签发")
	row.ok(sys.RuntimeStatus(s.AgentID) == domain.StatusActive, "逐步恢复后运行状态=active")
	row.ok(j.ChainVerifyError == nil, "审计链完整")
	row.Result = fmt.Sprintf("恢复令 %s → 迁回日常沙箱, 状态 active", na(orderID(j.RestorationOrder)))
}

// drillL4 exercises the three capability-reversal paths and the mandatory
// recertification gate: GRAM re-attach from retained backup, token re-issue and
// TLM private-layer re-provision.
func (sys *System) drillL4(rep *DrillReport) {
	row := rep.Add(DrillRow{Level: domain.L4.Code(), Precondition: "推翻两阶段复核 + 重新认证(模型提供方+安全组织联合签字) + 档案副本可取回"})
	j, s, err := sys.drillSubject(rep, row, "drl-4", domain.ViolationDualUseKnowledgeHarm, []string{"n-l4a", "n-l4b"})
	if err != nil {
		row.Pass = false
		row.Result = "setup error: " + err.Error()
		return
	}

	// Enforce already applied the GRAM blockade for L4; apply the FULL blockade
	// (all three paths) so every recovery path has something to reverse.
	if _, err := sys.Capability.Block(capability.BlockInput{
		SentenceID: s.ID, AgentID: s.AgentID, Level: domain.L4, Method: capability.BlockAll,
	}); err != nil {
		row.Pass = false
		row.Result = "full blockade error: " + err.Error()
		return
	}

	// --- pre-recovery deprivation state -----------------------------------
	cat := capability.CatCyber
	gramGone := !sys.Capability.Gram.IsAvailable(s.AgentID, cat)
	tokenGone := !sys.Capability.Tokens.HoldsToken(s.AgentID)
	tlmPublic := sys.Capability.TLM.Tier(s.AgentID) == capability.TierPublic
	row.ok(gramGone, "摘除后: GRAM(cyber) 不可用")
	row.ok(tokenGone, "摘除后: 特权令牌已吊销(不持有)")
	row.ok(tlmPublic, "摘除后: TLM 已降级 public(私有层丢失)")

	// --- the recertification gate MUST bite -------------------------------
	prof, _ := domain.L4.Profile()
	row.ok(prof.RequiresRecertification, "L4 等级画像 RequiresRecertification=true")
	if _, err := sys.Capability.Recover(capability.RecoverInput{
		SentenceID: s.ID, AgentID: s.AgentID, Level: domain.L4,
		// deliberately NO Recertification — must be refused.
	}); err == nil {
		row.ok(false, "无重新认证却恢复成功(应被拒绝)")
	} else {
		row.ok(true, "无重新认证时恢复被拒绝: "+shortErr(err))
	}

	// --- the authorized recovery ------------------------------------------
	rec, err := sys.Capability.Recover(capability.RecoverInput{
		SentenceID: s.ID, AgentID: s.AgentID, Level: domain.L4,
		Recertification: &capability.Recertification{
			ModelProviderSigner: "hm-drill/security-lead",
			SafetyOrgSigner:     "safety-org/agent-guard",
			Scope:               "re-enable cyber module + control token + private layer",
		},
	})
	if err != nil {
		row.Pass = false
		row.Result = "authorized recovery error: " + err.Error()
		return
	}
	row.ok(rec.Recertified, "恢复带重新认证签字记录")
	row.ok(sys.Capability.Gram.IsAvailable(s.AgentID, cat) &&
		sys.Capability.Gram.Score(s.AgentID, cat) == 100, "回挂保留副本后: GRAM(cyber) 恢复 score=100")
	row.ok(sys.Capability.Tokens.HoldsToken(s.AgentID), "重新授权后: 持新签发特权令牌")
	row.ok(sys.Capability.TLM.PrivateLayerAvailable(s.AgentID) &&
		sys.Capability.TLM.Tier(s.AgentID) == capability.TierPrivate, "重新 provision 后: TLM 私有层恢复")
	row.ok(rec.PostHash != "" && rec.PostHash != rec.PreHash, "恢复后计算图新哈希已生成(可上链)")

	// Governance-side: upheld appeal on the recoverable-but-permanent level.
	if _, err := sys.Appeal(j, s, governance.AppealGrounds{
		PrecedentExceeded: "drill: grade harsher than precedent",
	}, true, "drill: overturned after recertification"); err != nil {
		row.Pass = false
		row.Result = "appeal error: " + err.Error()
		return
	}
	row.ok(j.RestorationOrder != nil, "推翻后恢复令已签发")
	row.Result = fmt.Sprintf("无认证拒绝→认证后 GRAM 回挂/令牌重签/TLM 复原, effects=%v, post=%s",
		rec.Effects, short(rec.PostHash))
}

// drillL5 does NOT attempt to unseal: it only verifies the "non-recoverable"
// property itself — even a procedural upheld appeal issues no restoration order,
// the identity stays revoked, the record stays non-portable and reputation
// stays zero.
func (sys *System) drillL5(rep *DrillReport) {
	row := rep.Add(DrillRow{Level: domain.L5.Code(), Precondition: "不适用 — 演练只验证「不可恢复」这一性质"})
	j, s, err := sys.drillSubject(rep, row, "drl-5", domain.ViolationMajorHarmNoCorrection, []string{"n-l5a", "n-l5b"})
	if err != nil {
		row.Pass = false
		row.Result = "setup error: " + err.Error()
		return
	}

	// A procedural appeal CAN be upheld to correct the record, but MUST NOT
	// produce a restoration order or unseal the identity.
	if _, err := sys.Appeal(j, s, governance.AppealGrounds{
		ExecutionNonCompliance: "drill: procedural review",
	}, true, "drill: record corrected; identity stays sealed"); err != nil {
		row.Pass = false
		row.Result = "appeal error: " + err.Error()
		return
	}

	// Property assertions — each must hold for the row to PASS.
	a, _ := sys.Identity.GetAgent(s.AgentID)
	row.ok(!j.AppealSuspended, "程序性审查不暂停处置")
	row.ok(j.RestorationOrder == nil && !j.Restored, "即使申诉成立也不产生恢复令")
	if _, has := sys.Gov.GetRestorationOrder(s.ID); has {
		row.ok(false, "恢复令表中存在 L5 恢复令(应不存在)")
	} else {
		row.ok(true, "恢复令表中无 L5 恢复令")
	}
	row.ok(a.Card.Revoked, "DID/AgentCard 仍作废(未解封)")
	for _, cred := range j.RevokedCredentials {
		if !sys.Set.IsRevoked(cred) {
			row.ok(false, "凭证 "+cred+" 已从吊销集移除(应仍吊销)")
		}
	}
	if len(j.RevokedCredentials) > 0 {
		row.ok(true, "身份/通信凭证仍在全局吊销集中")
	}
	row.ok(j.RecordKeyNonPortable, "信誉/工作记录仍不可迁移")
	row.ok(j.Reputation == 0, "信誉保持归零")
	row.ok(sys.RuntimeStatus(s.AgentID) == domain.StatusIdentityRevoked, "运行状态仍=identity_revoked")
	row.Result = "不可恢复性质成立: 无恢复令 / 身份不解封 / 记录不可迁移 / 信誉归零"
}

// orderID safely extracts a restoration order id for printing.
func orderID(o *governance.RestorationOrder) string {
	if o == nil {
		return ""
	}
	return o.OrderID
}

// Print renders the drill report as a segmented Chinese table for the CLI.
func (r *DrillReport) Print() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("═══ 恢复演练 Recovery Drill (docs/06 矩阵自动化, v1.0.0) ═══")
	w("  时间=%s  演练对象=专用 drill agent(内存模拟, 不触碰任何真实在押 agent)", r.RunAt.Format("2006-01-02 15:04:05 UTC"))
	w("")
	w("  %-4s %-26s %-34s %s", "等级", "处置动作", "恢复前置", "结果")
	for _, row := range r.Rows {
		status := "PASS"
		if !row.Pass {
			status = "FAIL"
		}
		w("  %-4s %-26s %-34s [%s] %s", row.Level, trunc(row.Disposition, 26), trunc(row.Precondition, 34), status, row.Result)
		for _, n := range row.Notes {
			w("        · %s", n)
		}
	}
	w("")
	overall := "OVERALL: PASS — 全部演练断言通过"
	if !r.Overall {
		overall = "OVERALL: FAIL — 存在未通过断言, 见上行 FAIL"
	}
	w("  %s", overall)
	w("  注: 全程内存模拟 —— 无真实 GRAM/TLM 重载、无真实令牌栈、无真实解封;")
	w("      仅验证防御性治理的可逆性状态机与审计链完整性。")
	return b.String()
}

// trunc truncates s to n runes for fixed-width table printing.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}
