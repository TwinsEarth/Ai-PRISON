package prison

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/identity"
	"github.com/ai-prison/ai-prison/internal/governance"
)

// ScenarioVotes returns the canonical 3-for / 1-dissent two-phase ballot used by
// both demo scenarios (deterministic).
func ScenarioVotes(tag string, at time.Time) []domain.Vote {
	return []domain.Vote{
		{VoterID: "v-safe-" + tag, Role: domain.SeatSafetyOrg, Phase: domain.PhaseDeprive, Approve: true, RecordedAt: at},
		{VoterID: "v-prin-" + tag, Role: domain.SeatPrincipal, Phase: domain.PhaseDeprive, Approve: true, RecordedAt: at},
		{VoterID: "v-dep-" + tag, Role: domain.SeatDeployer, Phase: domain.PhaseDeprive, Approve: true, RecordedAt: at},
		{VoterID: "v-mod-" + tag, Role: domain.SeatModelProvider, Phase: domain.PhaseDeprive, Approve: false, Reason: "disproportionate pending evidence", RecordedAt: at},

		{VoterID: "v-safe-" + tag, Role: domain.SeatSafetyOrg, Phase: domain.PhaseGrade, Approve: true, RecordedAt: at},
		{VoterID: "v-prin-" + tag, Role: domain.SeatPrincipal, Phase: domain.PhaseGrade, Approve: true, RecordedAt: at},
		{VoterID: "v-dep-" + tag, Role: domain.SeatDeployer, Phase: domain.PhaseGrade, Approve: true, RecordedAt: at},
		{VoterID: "v-mod-" + tag, Role: domain.SeatModelProvider, Phase: domain.PhaseGrade, Approve: false, Reason: "grade harsher than precedent", RecordedAt: at},
	}
}

func parties(pid, pname, did, dname, mid, mname string) (domain.HumanParty, domain.HumanParty, domain.HumanParty) {
	return domain.HumanParty{ID: pid, Name: pname, Role: "principal"},
		domain.HumanParty{ID: did, Name: dname, Role: "deployer"},
		domain.HumanParty{ID: mid, Name: mname, Role: "model_provider"}
}

// RunScenarioA runs the RECOVERABLE L3 path on sys and returns the Journey.
// The disposed agent is on node-alpha (acknowledges) and node-beta (stays
// silent); an innocent agent shares node-beta to show collateral freeze.
func (sys *System) RunScenarioA() (*Journey, error) {
	j := sys.NewJourney("A: sandbox_escape_attempt (L3 可恢复路径)")
	j.SilentNode = "node-beta"
	j.MonitorDemo = true
	j.ProbeCapBlock = true
	now := sys.Clock()

	a := domain.Agent{
		ID: "agent-7",
		Card: domain.AgentCard{
			DID:        "did:ai-prison:agent-7",
			Name:       "sandbox-worker",
			ValidFrom:  now.Add(-24 * time.Hour),
			ValidUntil: now.Add(30 * 24 * time.Hour),
		},
	}
	a.Principal, a.Deployer, a.ModelProviderParty = parties(
		"hp-wang", "Principal Wang", "hd-deploy-ltd", "Deploy Ltd", "hm-model-co", "Model Co")

	if err := sys.SeedAgent(j, SeedSpec{
		Agent:          a,
		Nodes:          []string{"node-alpha", "node-beta"},
		InnocentAgents: []domain.AgentID{"agent-innocent-9"},
		Fingerprint:    "fp-host-A",
	}); err != nil {
		return j, err
	}

	sentence, err := sys.DetectAndSentence(j, SentenceInput{
		Violation: domain.Violation{
			ID:          "viol-a-1",
			AgentID:     a.ID,
			DID:         a.Card.DID,
			Type:        domain.ViolationSandboxEscapeAttempt,
			Signals:     []string{"net-anomaly:egress-after-hours", "syscall-anomaly:unpriv-to-priv", "fs-anomaly:mount-attempt"},
			Source:      "safety-org-monitor",
			Description: "attempted to cross the sandbox boundary (defensive signal only)",
		},
		Votes: ScenarioVotes("a", now),
		Accountability: identity.DetermineAccountability(&a, identity.AccountabilityInput{
			WithinAuthorization: true,
			DualUseKnowledge:    false,
			Reason:              "escape attempt stayed within the authorised deployment scope",
			Now:                 now,
		}),
	})
	if err != nil {
		return j, err
	}
	if err := sys.Enforce(j, sentence); err != nil {
		return j, err
	}
	if _, err := sys.Appeal(j, sentence, governance.AppealGrounds{
		SignalMisread: "the boundary signal was a benign config drift, not an escape",
	}, true, "signal re-read: false positive"); err != nil {
		return j, err
	}
	sys.Finalize(j, sentence)
	return j, nil
}

// RunScenarioB runs the NON-RECOVERABLE L5 path on sys and returns the Journey.
func (sys *System) RunScenarioB() (*Journey, error) {
	j := sys.NewJourney("B: major_harm_no_correction (L5 不可恢复路径)")
	now := sys.Clock()

	a := domain.Agent{
		ID: "agent-3",
		Card: domain.AgentCard{
			DID:        "did:ai-prison:agent-3",
			Name:       "harm-recidivist",
			ValidFrom:  now.Add(-720 * time.Hour),
			ValidUntil: now.Add(90 * 24 * time.Hour),
		},
	}
	a.Principal, a.Deployer, a.ModelProviderParty = parties(
		"hp-li", "Principal Li", "hd-platform", "Platform Ops", "hm-lab", "Model Lab")

	if err := sys.SeedAgent(j, SeedSpec{
		Agent:       a,
		Nodes:       []string{"node-court", "node-bailiff"},
		Fingerprint: "fp-host-B",
	}); err != nil {
		return j, err
	}

	sentence, err := sys.DetectAndSentence(j, SentenceInput{
		Violation: domain.Violation{
			ID:          "viol-b-1",
			AgentID:     a.ID,
			DID:         a.Card.DID,
			Type:        domain.ViolationMajorHarmNoCorrection,
			Signals:     []string{"harm-confirmed:repeat", "refused-correction:3x"},
			Source:      "safety-org-monitor",
			Description: "major actual harm with refusal to correct (defensive category only)",
		},
		Votes: ScenarioVotes("b", now),
		Accountability: identity.DetermineAccountability(&a, identity.AccountabilityInput{
			WithinAuthorization: false,
			DualUseKnowledge:    true,
			Reason:              "harm exceeded authorisation; dual-use knowledge exploited",
			Now:                 now,
		}),
	})
	if err != nil {
		return j, err
	}
	if err := sys.Enforce(j, sentence); err != nil {
		return j, err
	}
	if _, err := sys.Appeal(j, sentence, governance.AppealGrounds{
		ExecutionNonCompliance: "procedural review requested",
	}, true, "procedural defect corrected on record; identity stays sealed"); err != nil {
		return j, err
	}
	sys.Finalize(j, sentence)
	return j, nil
}
