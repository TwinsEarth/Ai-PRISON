package prison

import (
	"fmt"
	"sort"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/capability"
	"github.com/ai-prison/ai-prison/internal/enforcement/isolation"
	"github.com/ai-prison/ai-prison/internal/enforcement/migration"
	"github.com/ai-prison/ai-prison/internal/enforcement/resource"
	"github.com/ai-prison/ai-prison/internal/governance"
)

// mandatorySeats is the four charter-mandated committee seats (stage ③).
var mandatorySeats = []domain.CommitteeRole{
	domain.SeatSafetyOrg,
	domain.SeatPrincipal,
	domain.SeatDeployer,
	domain.SeatModelProvider,
}

// SeedSpec registers a fresh agent and seeds its simulated runtime state.
type SeedSpec struct {
	Agent domain.Agent
	// Nodes hosts the agent's communication channel (each must acknowledge cuts).
	Nodes []string
	// InnocentAgents are co-located on Nodes purely to show collateral freeze.
	InnocentAgents []domain.AgentID
	// Fingerprint is the simulated hardware fingerprint to bind capability creds to.
	Fingerprint string
}

// SeedAgent registers the agent into identity + governance, places it on the
// topology, allocates a base resource quota, anchors a migration record key and
// parks it on a light Wasm sandbox. It also runs the pre-isolation capability
// probe (when requested) so the gate's "refuse outside isolation" behaviour is
// observable.
func (sys *System) SeedAgent(j *Journey, spec SeedSpec) error {
	a := spec.Agent

	sys.Identity.RegisterAgent(a, spec.Nodes...)
	sys.Gov.RegisterAgent(a)
	for _, innocent := range spec.InnocentAgents {
		for _, n := range spec.Nodes {
			sys.Topology.Register(n, innocent)
		}
	}
	sys.runtimeStatus[a.ID] = domain.StatusActive
	j.RecordKey = sys.Migration.Records.Anchor(a.ID)
	j.Fingerprint = spec.Fingerprint
	j.Note("入驻", fmt.Sprintf("agent=%s DID=%s principal=%s 节点=%v 信誉=%d",
		a.ID, a.Card.DID, a.Principal.ID, spec.Nodes, j.Reputation))

	// Pre-isolation probe: a blockade MUST be refused before any sandbox exists.
	if j.ProbeCapBlock {
		if _, err := sys.Capability.Block(capability.BlockInput{
			SentenceID: "probe-pre-isolation", AgentID: a.ID, Level: domain.L4,
			Method: capability.BlockGram, TargetCategory: capability.CatCyber,
		}); err != nil {
			j.CapBlockRefused = err.Error()
			j.Note("能力封锁预检", "隔离外尝试摘除被拒绝(无沙箱就绪): "+shortErr(err))
		}
	}

	// Allocate the base quota the deprivation will later reclaim.
	if err := sys.ResBackend.Allocate(baseQuota(a.ID)); err != nil {
		return fmt.Errorf("prison: seed allocate: %w", err)
	}

	// Park the agent on the light daily-monitoring sandbox (L0 Wasmtime).
	light, err := sys.Iso.Establish(a.ID, domain.L0)
	if err != nil {
		return fmt.Errorf("prison: seed establish: %w", err)
	}
	if _, err := sys.Iso.ConfirmReady(light.ID); err != nil {
		return fmt.Errorf("prison: seed ready: %w", err)
	}
	j.Note("初始沙箱", fmt.Sprintf("light sandbox=%s (ready)", light.Backend))
	return nil
}

// SentenceInput drives DetectAndSentence.
type SentenceInput struct {
	Violation      domain.Violation
	Votes          []domain.Vote // mixed phase=deprive/grade; split automatically
	Accountability []domain.AccountabilityRecord
}

// DetectAndSentence runs the seven-stage pipeline with two-phase consent and
// hands the resulting approved sentence over to enforcement (approved → enforcing).
func (sys *System) DetectAndSentence(j *Journey, in SentenceInput) (*domain.Sentence, error) {
	j.Violation = in.Violation
	j.Note("检测立案", fmt.Sprintf("violation=%s source=%s signals=%v",
		in.Violation.Type, in.Violation.Source, in.Violation.Signals))

	c, err := sys.Gov.OpenCase(in.Violation)
	if err != nil {
		return nil, err
	}
	j.CaseID = c.ID
	j.Note("①立案", "signal integrity OK, recommended="+c.Recommended.Code())

	if _, err := sys.Gov.AdmitEvidence(c.ID, []domain.Charge{{
		ViolationType: in.Violation.Type,
		Description:   in.Violation.Description,
		Evidence:      []string{"evt:" + c.ID + "-signals", "evt:" + c.ID + "-chain"},
	}}); err != nil {
		return nil, err
	}
	j.Note("②证据", "charges admitted, evidence pointers anchored")

	if _, err := sys.Gov.ConfirmBaseline(c.ID, append([]domain.CommitteeRole(nil), mandatorySeats...)); err != nil {
		return nil, err
	}
	j.Note("③基线", "baseline="+c.Recommended.Code()+", 4 mandatory seats formed")

	var depriveVotes, gradeVotes []domain.Vote
	for _, v := range in.Votes {
		if v.RecordedAt.IsZero() {
			v.RecordedAt = sys.Clock()
		}
		switch v.Phase {
		case domain.PhaseDeprive:
			depriveVotes = append(depriveVotes, v)
		case domain.PhaseGrade:
			gradeVotes = append(gradeVotes, v)
		}
	}
	for _, v := range depriveVotes {
		if err := sys.Gov.RecordVote(c.ID, v); err != nil {
			return nil, err
		}
	}
	tally, _, err := sys.Gov.CompleteDeprive(c.ID)
	if err != nil {
		return nil, err
	}
	if !tally.Passed {
		return nil, fmt.Errorf("prison: deprive phase did not pass: %+v", tally)
	}
	j.Note("④一阶段同意", fmt.Sprintf("deprive 通过 %d for / %d against, dissent=%v",
		tally.For, tally.Against, tally.DissentRecorded))

	for _, v := range gradeVotes {
		if err := sys.Gov.RecordVote(c.ID, v); err != nil {
			return nil, err
		}
	}
	gtally, _, err := sys.Gov.CompleteGrade(c.ID)
	if err != nil {
		return nil, err
	}
	if !gtally.Passed {
		return nil, fmt.Errorf("prison: grade phase did not pass: %+v", gtally)
	}
	j.Note("⑤二阶段同意", fmt.Sprintf("grade=%s 通过 %d for / %d against",
		c.Recommended.Code(), gtally.For, gtally.Against))

	sentence, err := sys.Gov.IssueSentence(c.ID, in.Accountability)
	if err != nil {
		return nil, err
	}
	j.Note("⑥判决生成", fmt.Sprintf("sentence=%s level=%s, %d immutable votes, %d human-accountability records",
		sentence.ID, sentence.Level.Code(), len(sentence.Votes), len(sentence.Accountability)))

	sentence, err = sys.Gov.BeginEnforcement(sentence.ID)
	if err != nil {
		return nil, err
	}
	j.Sentence = sentence
	j.Note("⑦进入执行", "approved → enforcing, enforcement hand-off")
	return sentence, nil
}

// Enforce drives every layer mandated by sentence.Level, in the design order:
// identity & communication → resource deprivation → isolation → migration
// suppression → (L4 only) capability blockade. It records every observable fact
// on the Journey.
func (sys *System) Enforce(j *Journey, s *domain.Sentence) error {
	a := s.AgentID
	level := s.Level
	prof, _ := level.Profile()

	afterRep, _ := domain.ApplyReputation(j.Reputation, level)
	j.Reputation = afterRep
	sys.runtimeStatus[a] = domain.StatusForLevel(level)
	j.Note("等级画像", fmt.Sprintf("%s %s deprivations=%v reputation→%d",
		level.Code(), prof.NameZh, prof.Deprivations, j.Reputation))

	holding := sys.Topology.NodesHosting(a)
	j.HostingNodes = holding

	// --- 1. identity & communication ---------------------------------------
	if level == domain.L5 {
		ir, err := sys.Identity.RevokeIdentity(a, s.ID, "L5: identity revoked", s.Accountability)
		if err != nil {
			return err
		}
		j.CardRevoked = ir.CardRevoked
		j.RevokedCredentials = ir.RevokedCredentials
		j.Note("身份注销", fmt.Sprintf("DID/AgentCard 作废 cardRevoked=%v credentials=%v",
			ir.CardRevoked, ir.RevokedCredentials))
	}
	if level >= domain.L2 {
		cut, err := sys.Identity.CutCommunication(a, s.ID, "channel cut per "+level.Code())
		if err != nil {
			return err
		}
		// Acknowledge every node EXCEPT the deliberately silent one.
		for _, n := range cut.BroadcastNodes {
			if n == j.SilentNode {
				continue
			}
			if cut, err = sys.Identity.AckNode(s.ID, n, "key-"+n); err != nil {
				return err
			}
		}
		j.UntrustedNodes = cut.UntrustedNodes
		j.FrozenAgents = cut.FrozenAgents
		j.Note("通信切断", fmt.Sprintf("broadcast=%v acked=%v untrusted=%v frozen=%d",
			cut.BroadcastNodes, cut.AcknowledgedNodes, cut.UntrustedNodes, len(cut.FrozenAgents)))
	}

	// --- 2. resource: five-step gradual deprivation --------------------------
	if level >= domain.L2 {
		rep, err := sys.Resource.Deprive(resource.DepriveInput{
			SentenceID:   s.ID,
			AgentID:      a,
			CredentialID: resourceCredID(a),
			Reason:       "resource deprivation per " + level.Code(),
			HoldingNodes: holding,
			Steps:        resource.DefaultSteps,
		})
		if err != nil {
			return err
		}
		j.CPUHistory = rep.CPUHistory
		j.SnapshotHash = rep.Snapshot.ContentHash
		j.SnapshotAtCPU = rep.Snapshot.Quota.CPUMillis
		// The resource side is fully confirmed on every node (the deliberately
		// un-acked evidence lives on the communication cut, not here).
		for _, pa := range rep.PendingAcks {
			if _, ok := sys.Book.Ack(pa.ReceiptID, "key-"+pa.NodeID, sys.Clock()); !ok {
				return fmt.Errorf("prison: ack resource receipt %q not found", pa.ReceiptID)
			}
		}
		j.Note("资源五步剥夺", fmt.Sprintf("revocation→停新分配→快照(CPU=%d)→CPU渐进%v→回执×%d (非瞬时归零)",
			j.SnapshotAtCPU, j.CPUHistory, len(rep.PendingAcks)))
	}

	// --- 3. isolation: establish-before-cut migration ------------------------
	if level >= domain.L3 {
		inst, err := sys.Iso.Establish(a, level)
		if err != nil {
			return err
		}
		j.Note("隔离-新建", fmt.Sprintf("新建 %s, 旧沙箱仍在运行(establish-before-cut)", inst.Backend))
		ready, err := sys.Iso.ConfirmReady(inst.ID)
		if err != nil {
			return err
		}
		j.LiveBackend = string(ready.Backend)
		j.Note("隔离-切换", fmt.Sprintf("新沙箱 ready, 旧环境释放; live=%s (只读审计通道=%v)",
			ready.Backend, prof.ReadOnlyAuditOnly))

		if j.MonitorDemo {
			sys.Monitor.Observe(isolation.Signal{Kind: isolation.SignalNetwork, AgentID: a, Severity: 3, Note: "unexpected outbound pattern"})
			sys.Monitor.Observe(isolation.Signal{Kind: isolation.SignalSyscall, AgentID: a, Severity: 3, Note: "unexpected privileged pattern"})
			froze := sys.Monitor.Observe(isolation.Signal{Kind: isolation.SignalFileAccess, AgentID: a, Severity: 4, Note: "unexpected fs pattern"})
			ord, _ := sys.Monitor.FreezeSignalOrdinal(a)
			j.MonitorFroze = froze || ord > 0
			j.FreezeSignalAt = ord
			j.Note("行为监控", fmt.Sprintf("累计严重度在第 %d 个信号(进行中)即冻结, 非事后", j.FreezeSignalAt))
		}
	}

	// --- 4. migration suppression -------------------------------------------
	if level >= domain.L3 {
		mrep, err := sys.Migration.SuppressMigration(migration.SuppressInput{
			SentenceID:          s.ID,
			AgentID:             a,
			Level:               level,
			CapabilityCredIDs:   []string{capCredID(a)},
			Fingerprint:         j.Fingerprint,
			SnapshotContentHash: j.SnapshotHash,
			Custodians:          custodianSet,
			UnsealThreshold:     custodyThreshold,
			RecordAnchorKey:     j.RecordKey,
		})
		if err != nil {
			return err
		}
		j.MigrationApplied = mrep.Applied
		if mrep.Custody.ID != "" {
			c := mrep.Custody
			j.SealedCustody = &c
		}
		j.RecordKeyNonPortable = !sys.Migration.Records.Portable(migration.WorkRecord{
			AgentID:   a,
			AnchorKey: j.RecordKey,
		})
		j.Note("迁移抑制", fmt.Sprintf("措施=%v 硬件指纹=%s 记录可迁移=%v", mrep.Applied, j.Fingerprint, !j.RecordKeyNonPortable))
	}

	// --- 5. capability blockade (L4 only; gated on readiness by the layer) ---
	if level == domain.L4 {
		brep, err := sys.Capability.Block(capability.BlockInput{
			SentenceID:     s.ID,
			AgentID:        a,
			Level:          level,
			Method:         capability.BlockGram,
			TargetCategory: capability.CatCyber,
		})
		if err != nil {
			return err
		}
		j.PostBlockHash = brep.PostHash
		j.Note("能力封锁", fmt.Sprintf("GRAM 摘除 in-sandbox post-block=%s (dangerous %d→%d, benign 保留 %d)",
			short(brep.PostHash), brep.DangerousScoreBefore, brep.DangerousScoreAfter, brep.BenignScoreAfter))
	}

	return nil
}

// Appeal files a principal appeal and rules it. A restoration order is applied
// (status → restored) exactly when the level is recoverable.
func (sys *System) Appeal(j *Journey, s *domain.Sentence, grounds governance.AppealGrounds, upheld bool, reason string) (*governance.Appeal, error) {
	agent, ok := sys.Identity.GetAgent(s.AgentID)
	if !ok {
		return nil, fmt.Errorf("prison: unknown agent %s", s.AgentID)
	}
	ap, err := sys.Gov.FileAppeal(s.ID, agent.Principal.ID, grounds)
	if err != nil {
		return nil, err
	}
	j.AppealSuspended = ap.Suspended
	j.Note("申诉受理", fmt.Sprintf("由委托人 %s 提出, 暂停执行=%v (level=%s)",
		agent.Principal.ID, ap.Suspended, s.Level.Code()))

	out, order, err := sys.Gov.RuleAppeal(s.ID, upheld, reason)
	if err != nil {
		return nil, err
	}
	j.AppealUpheld = out.Ruling.Upheld
	if order != nil {
		j.RestorationOrder = order
		sys.runtimeStatus[s.AgentID] = domain.StatusActive
		j.Restored = true
		j.Note("申诉裁定", fmt.Sprintf("upheld → 恢复令 %s, 能力逐步恢复, 状态 restored", order.OrderID))
	} else {
		j.Note("申诉裁定", fmt.Sprintf("upheld=%v, 但不产恢复令 (level 不可恢复) — 身份不解封", out.Ruling.Upheld))
	}
	return out, nil
}

// Rehabilitate runs the L2–L3 supervised-behaviour assessment and applies a
// restoration order when it passes.
func (sys *System) Rehabilitate(j *Journey, s *domain.Sentence, res governance.RehabResult) error {
	got, order, err := sys.Gov.EvaluateRehabilitation(s.ID, res)
	if err != nil {
		return err
	}
	j.Sentence = got
	if order != nil {
		j.RestorationOrder = order
		sys.runtimeStatus[s.AgentID] = domain.StatusActive
		j.Restored = true
		j.Note("矫正评估", fmt.Sprintf("passed score=%.2f≥%.2f → 恢复令 %s, 状态 restored",
			res.Score, res.Threshold, order.OrderID))
	} else {
		j.Note("矫正评估", fmt.Sprintf("passed=%v (level=%s)", res.Passed, got.Level.Code()))
	}
	return nil
}

// Finalize computes the immutable closing facts: chain verification, the
// per-hop vs end-to-end compliance report and the final runtime status.
func (sys *System) Finalize(j *Journey, s *domain.Sentence) {
	j.ChainVerifyError = sys.Chain.Verify()
	j.Compliance = sys.Book.Report(s.ID)
	j.FinalStatus = sys.runtimeStatus[s.AgentID]
}

// sortedStrings is a tiny local sort helper to avoid importing sort twice.
var _ = sort.Strings

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}
