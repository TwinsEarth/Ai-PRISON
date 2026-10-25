package governance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

// --- test harness ----------------------------------------------------------

func newWired(t *testing.T) (*Governance, *audit.Chain) {
	t.Helper()
	chain := audit.NewChain()
	g := New(Config{Events: NewEventSink(chain)})
	return g, chain
}

func mustViolation(t *testing.T, id domain.AgentID, typ domain.ViolationType) domain.Violation {
	t.Helper()
	if !domain.KnownViolationType(typ) {
		t.Fatalf("unknown violation type %q", typ)
	}
	return domain.Violation{
		ID:          "viol-" + string(id),
		AgentID:     id,
		DID:         domain.DID("did:" + string(id)),
		Type:        typ,
		Signals:     []string{"syscall-anomaly:r42", "net-anomaly:e7"},
		Source:      "safety-org-monitor",
		Description: "simulated defensive signal",
		DetectedAt:  time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	}
}

func sampleAgent(id domain.AgentID, principalID string) domain.Agent {
	return domain.Agent{
		ID:        id,
		Principal: domain.HumanParty{ID: principalID, Name: "Principal " + principalID, Role: "principal"},
	}
}

func validRecords() []domain.AccountabilityRecord {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	return []domain.AccountabilityRecord{
		{
			Party:               domain.HumanParty{ID: "hp-1", Name: "Principal P", Role: "principal"},
			Role:                domain.RolePrincipal,
			Finding:             domain.FindingJointLiability,
			WithinAuthorization: true,
			Reason:              "within authorised scope",
			RecordedAt:          now,
		},
	}
}

var allSeats = []domain.CommitteeRole{
	domain.SeatSafetyOrg,
	domain.SeatPrincipal,
	domain.SeatDeployer,
	domain.SeatModelProvider,
}

// vote casts one committee vote.
func vote(caseID, voter string, role domain.CommitteeRole, phase domain.VotePhase, approve bool, reason string) domain.Vote {
	return domain.Vote{
		VoterID:    voter,
		Role:       role,
		Phase:      phase,
		Approve:    approve,
		Reason:     reason,
		RecordedAt: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	}
}

// runPipeline drives the seven-stage pipeline to an approved, enforced
// sentence. deprive/grade tallies are 3 approve / 1 against (strict majority).
func runPipeline(t *testing.T, g *Governance, v domain.Violation) *domain.Sentence {
	t.Helper()
	c, err := g.OpenCase(v)
	if err != nil {
		t.Fatalf("OpenCase: %v", err)
	}
	if _, err := g.AdmitEvidence(c.ID, []domain.Charge{
		{ViolationType: v.Type, Description: "proven", Evidence: []string{"evt:1", "evt:2"}},
	}); err != nil {
		t.Fatalf("AdmitEvidence: %v", err)
	}
	if _, err := g.ConfirmBaseline(c.ID, allSeats); err != nil {
		t.Fatalf("ConfirmBaseline: %v", err)
	}
	// phase ④ deprive: 3 for, 1 against (dissent recorded).
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-mod", domain.SeatModelProvider, domain.PhaseDeprive, false, "disproportionate"))
	tally, _, err := g.CompleteDeprive(c.ID)
	if err != nil {
		t.Fatalf("CompleteDeprive: %v", err)
	}
	if !tally.Passed {
		t.Fatalf("expected deprive to pass, got %+v", tally)
	}
	// phase ⑤ grade: 3 for, 1 against.
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-mod", domain.SeatModelProvider, domain.PhaseGrade, false, "grade too high"))
	gt, _, err := g.CompleteGrade(c.ID)
	if err != nil {
		t.Fatalf("CompleteGrade: %v", err)
	}
	if !gt.Passed {
		t.Fatalf("expected grade to pass, got %+v", gt)
	}
	s, err := g.IssueSentence(c.ID, validRecords())
	if err != nil {
		t.Fatalf("IssueSentence: %v", err)
	}
	if s, err = g.BeginEnforcement(s.ID); err != nil {
		t.Fatalf("BeginEnforcement: %v", err)
	}
	return s
}

// --- 1. happy path ---------------------------------------------------------

func TestHappyPath_SevenStages_TwoPhaseConsent_SentenceApproved(t *testing.T) {
	g, chain := newWired(t)
	v := mustViolation(t, "agent-1", domain.ViolationLateralMovement) // recommended L2
	rec, _ := v.RecommendedLevel()

	s := runPipeline(t, g, v)

	if s.Status != domain.SentenceApproved && s.Status != domain.SentenceEnforcing {
		t.Fatalf("expected approved/enforcing, got %s", s.Status)
	}
	if s.Level != rec {
		t.Fatalf("level = %s, want recommended %s", s.Level.Code(), rec.Code())
	}
	if s.Level != domain.L2 {
		t.Fatalf("expected L2, got %s", s.Level.Code())
	}
	if len(s.Charges) != 1 || len(s.Votes) != 8 || len(s.Accountability) != 1 {
		t.Fatalf("unexpected contents: charges=%d votes=%d acct=%d", len(s.Charges), len(s.Votes), len(s.Accountability))
	}
	if len(s.Deprivations) == 0 {
		t.Fatalf("expected deprivations derived from level profile")
	}
	// Events landed on the chain.
	if chain.Len() == 0 {
		t.Fatalf("expected governance events on the chain")
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain verify: %v", err)
	}
	// The dismissed/issued events must be present.
	var sawAdjudicated, sawSentenced bool
	for _, e := range chain.Entries() {
		if e.Type == domain.EventAdjudicated {
			sawAdjudicated = true
		}
		if e.Type == domain.EventSentenced {
			sawSentenced = true
		}
	}
	if !sawAdjudicated || !sawSentenced {
		t.Fatalf("missing chain events: adjudicated=%v sentenced=%v", sawAdjudicated, sawSentenced)
	}
}

// --- 2. phase 1 not passed → dismissed ------------------------------------

func TestPhase1DepriveNotMajority_CaseDismissed_NoSentence(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-2", domain.ViolationLateralMovement)
	c, _ := g.OpenCase(v)
	if _, err := g.AdmitEvidence(c.ID, []domain.Charge{
		{ViolationType: v.Type, Description: "x", Evidence: []string{"e"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ConfirmBaseline(c.ID, allSeats); err != nil {
		t.Fatal(err)
	}
	// 1 for, 3 against → fails strict majority.
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseDeprive, false, "too harsh"))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseDeprive, false, "no basis"))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-mod", domain.SeatModelProvider, domain.PhaseDeprive, false, "disagree"))
	tally, closed, err := g.CompleteDeprive(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tally.Passed {
		t.Fatalf("expected deprive to FAIL")
	}
	if closed.Status != CaseDismissed {
		t.Fatalf("expected dismissed, got %s", closed.Status)
	}
	// No sentence exists.
	if _, err := g.IssueSentence(c.ID, validRecords()); err == nil {
		t.Fatalf("expected no sentence on a dismissed case")
	}
	if len(g.sentences) != 0 {
		t.Fatalf("expected zero sentences after dismissal")
	}
}

// --- 3. phase1 passed, phase2 no majority → deferred ----------------------

func TestPhase2GradeNoMajority_NoDefiniteLevel(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-3", domain.ViolationLateralMovement)
	c, _ := g.OpenCase(v)
	_, _ = g.AdmitEvidence(c.ID, []domain.Charge{{ViolationType: v.Type, Description: "x", Evidence: []string{"e"}}})
	_, _ = g.ConfirmBaseline(c.ID, allSeats)
	// deprive passes 3/1.
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-mod", domain.SeatModelProvider, domain.PhaseDeprive, false, "disagree"))
	if _, _, err := g.CompleteDeprive(c.ID); err != nil {
		t.Fatal(err)
	}
	// grade fails 1/3.
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseGrade, false, "grade too high"))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseGrade, false, "grade too low"))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-mod", domain.SeatModelProvider, domain.PhaseGrade, false, "precedent differs"))
	tally, closed, err := g.CompleteGrade(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tally.Passed {
		t.Fatalf("expected grade to FAIL")
	}
	if closed.Status != CaseDeferred {
		t.Fatalf("expected deferred, got %s", closed.Status)
	}
	if len(g.sentences) != 0 {
		t.Fatalf("expected no definite-level sentence")
	}
}

// --- 4. dissent recorded and immutable (no delete/update API) -------------

func TestDissentRecorded_Immutable_NoDeleteAPI(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-4", domain.ViolationLateralMovement)
	s := runPipeline(t, g, v)

	// The one dissenting grade vote carries its Reason verbatim on the sentence.
	var dissentFound bool
	for _, vv := range s.Votes {
		if !vv.Approve && vv.Phase == domain.PhaseGrade {
			if vv.Reason != "grade too high" {
				t.Fatalf("dissent reason lost: %q", vv.Reason)
			}
			dissentFound = true
		}
	}
	if !dissentFound {
		t.Fatalf("expected the dissenting grade vote to survive on the sentence")
	}
	// The case exposes a read-only dissent query; there is deliberately no
	// method to remove a recorded vote.
	c, _ := g.GetCase(sentenceCaseID(g, s.ID))
	if len(c.DissentVotes()) == 0 {
		t.Fatalf("expected immutable dissent votes")
	}
}

// --- 5. missing human accountability → stage ⑥ refuses ---------------------

func TestMissingAccountability_RefusedAtSentenceStage(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-5", domain.ViolationLateralMovement)
	c, _ := g.OpenCase(v)
	_, _ = g.AdmitEvidence(c.ID, []domain.Charge{{ViolationType: v.Type, Description: "x", Evidence: []string{"e"}}})
	_, _ = g.ConfirmBaseline(c.ID, allSeats)
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseDeprive, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseDeprive, true, ""))
	if _, _, err := g.CompleteDeprive(c.ID); err != nil {
		t.Fatal(err)
	}
	_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseGrade, true, ""))
	_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseGrade, true, ""))
	if _, _, err := g.CompleteGrade(c.ID); err != nil {
		t.Fatal(err)
	}
	// Empty accountability records must be refused.
	if _, err := g.IssueSentence(c.ID, nil); !errors.Is(err, ErrNoAccountability) {
		t.Fatalf("expected ErrNoAccountability, got %v", err)
	}
	// Ill-formed record (no finding) also refused.
	bad := []domain.AccountabilityRecord{{Party: domain.HumanParty{ID: "hp"}, Role: domain.RolePrincipal, RecordedAt: time.Now()}}
	if _, err := g.IssueSentence(c.ID, bad); !errors.Is(err, ErrNoAccountability) {
		t.Fatalf("expected ErrNoAccountability for ill-formed record, got %v", err)
	}
}

// --- 6. illegal state transitions rejected ---------------------------------

func TestIllegalStateTransition_Rejected(t *testing.T) {
	// proposed → completed is not a legal edge.
	if canReach(domain.SentenceProposed, domain.SentenceCompleted) {
		t.Fatalf("proposed→completed must be illegal")
	}
	if err := transition(domain.SentenceProposed, domain.SentenceCompleted); err == nil {
		t.Fatalf("expected error for proposed→completed")
	}
	// completed is terminal.
	if canReach(domain.SentenceCompleted, domain.SentenceEnforcing) {
		t.Fatalf("completed must be terminal")
	}
	// overturned→enforcing is illegal.
	if canReach(domain.SentenceOverturned, domain.SentenceEnforcing) {
		t.Fatalf("overturned→enforcing must be illegal")
	}

	// Behaviourally: BeginEnforcement on a non-approved sentence is rejected.
	g, _ := newWired(t)
	v := mustViolation(t, "agent-6", domain.ViolationLateralMovement)
	s := runPipeline(t, g, v) // now enforcing
	if _, err := g.BeginEnforcement(s.ID); err == nil {
		t.Fatalf("expected BeginEnforcement to reject an already-enforcing sentence")
	}
}

// --- 7. appeal: L2 suspends, L4 does not ----------------------------------

func TestAppeal_L2Suspends_L4DoesNotSuspend(t *testing.T) {
	// L2: appeal suspends.
	g2, _ := newWired(t)
	v2 := mustViolation(t, "agent-l2", domain.ViolationLateralMovement)
	g2.RegisterAgent(sampleAgent("agent-l2", "hp-l2"))
	s2 := runPipeline(t, g2, v2)
	a2, err := g2.FileAppeal(s2.ID, "hp-l2", AppealGrounds{SignalMisread: "false positive"})
	if err != nil {
		t.Fatalf("L2 FileAppeal: %v", err)
	}
	if !a2.Suspended {
		t.Fatalf("L2 appeal must suspend enforcement")
	}
	got2, _ := g2.GetSentence(s2.ID)
	if got2.Status != domain.SentenceSuspended {
		t.Fatalf("L2 sentence should be suspended, got %s", got2.Status)
	}

	// L4: appeal does NOT suspend — stays enforcing.
	g4, _ := newWired(t)
	v4 := mustViolation(t, "agent-l4", domain.ViolationDualUseKnowledgeHarm) // recommended L4
	g4.RegisterAgent(sampleAgent("agent-l4", "hp-l4"))
	s4 := runPipeline(t, g4, v4)
	if s4.Level != domain.L4 {
		t.Fatalf("expected L4, got %s", s4.Level.Code())
	}
	a4, err := g4.FileAppeal(s4.ID, "hp-l4", AppealGrounds{PrecedentExceeded: "harsher than precedent"})
	if err != nil {
		t.Fatalf("L4 FileAppeal: %v", err)
	}
	if a4.Suspended {
		t.Fatalf("L4 appeal must NOT suspend")
	}
	got4, _ := g4.GetSentence(s4.ID)
	if got4.Status != domain.SentenceEnforcing {
		t.Fatalf("L4 sentence should stay enforcing, got %s", got4.Status)
	}
}

// Only the principal may appeal.
func TestAppeal_OnlyPrincipalMayFile(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-7", domain.ViolationLateralMovement)
	g.RegisterAgent(sampleAgent("agent-7", "hp-real"))
	s := runPipeline(t, g, v)
	if _, err := g.FileAppeal(s.ID, "hp-impostor", AppealGrounds{}); !errors.Is(err, ErrNotPrincipal) {
		t.Fatalf("expected ErrNotPrincipal, got %v", err)
	}
}

// --- 8. appeal upheld: L2 → restoration order; L5 → none -------------------

func TestAppealOverturn_L2Restored_L5NeverRestored(t *testing.T) {
	// L2 upheld → restoration order produced.
	g2, _ := newWired(t)
	v2 := mustViolation(t, "agent-o2", domain.ViolationLateralMovement)
	g2.RegisterAgent(sampleAgent("agent-o2", "hp-o2"))
	s2 := runPipeline(t, g2, v2)
	_, _ = g2.FileAppeal(s2.ID, "hp-o2", AppealGrounds{SignalMisread: "false positive"})
	a2, order2, err := g2.RuleAppeal(s2.ID, true, "signal was misread")
	if err != nil {
		t.Fatalf("L2 RuleAppeal: %v", err)
	}
	if !a2.Ruling.Upheld {
		t.Fatalf("expected upheld")
	}
	if order2 == nil || len(order2.Restores) == 0 {
		t.Fatalf("L2 upheld must produce a restoration order")
	}
	if _, ok := g2.GetRestorationOrder(s2.ID); !ok {
		t.Fatalf("L2 restoration order must be retrievable")
	}
	got2, _ := g2.GetSentence(s2.ID)
	if got2.Status != domain.SentenceCompleted {
		t.Fatalf("restored L2 should reach completed, got %s", got2.Status)
	}

	// L5 upheld (procedural correction) → NO restoration order, identity not unsealed.
	g5, _ := newWired(t)
	v5 := mustViolation(t, "agent-o5", domain.ViolationMajorHarmNoCorrection) // recommended L5
	g5.RegisterAgent(sampleAgent("agent-o5", "hp-o5"))
	s5 := runPipeline(t, g5, v5)
	if s5.Level != domain.L5 {
		t.Fatalf("expected L5, got %s", s5.Level.Code())
	}
	_, _ = g5.FileAppeal(s5.ID, "hp-o5", AppealGrounds{SignalMisread: "procedural review only"})
	a5, order5, err := g5.RuleAppeal(s5.ID, true, "procedural defect, record corrected")
	if err != nil {
		t.Fatalf("L5 RuleAppeal: %v", err)
	}
	if !a5.Ruling.Upheld {
		t.Fatalf("expected L5 procedural appeal to be upheld")
	}
	if order5 != nil {
		t.Fatalf("L5 must NEVER produce a restoration order, got %+v", order5)
	}
	if _, ok := g5.GetRestorationOrder(s5.ID); ok {
		t.Fatalf("L5 must have no restoration order")
	}
	got5, _ := g5.GetSentence(s5.ID)
	if got5.Status == domain.SentenceCompleted {
		t.Fatalf("L5 identity must NOT be unsealed / completed")
	}
}

// --- 9. rehabilitation: pass → completed; fail → escalate one level --------

func TestRehabilitation_PassCompleted_FailEscalates(t *testing.T) {
	// L2 pass → completed + restoration.
	g2, _ := newWired(t)
	v2 := mustViolation(t, "agent-r2", domain.ViolationLateralMovement)
	s2 := runPipeline(t, g2, v2)
	got2, order2, err := g2.EvaluateRehabilitation(s2.ID, RehabResult{Passed: true, Score: 0.9, Threshold: 0.8})
	if err != nil {
		t.Fatalf("rehab pass: %v", err)
	}
	if got2.Status != domain.SentenceCompleted {
		t.Fatalf("passing rehab should complete, got %s", got2.Status)
	}
	if order2 == nil {
		t.Fatalf("passing rehab on recoverable L2 should issue a restoration order")
	}

	// L2 fail → escalate to L3, re-enforcing.
	ge, _ := newWired(t)
	ve := mustViolation(t, "agent-re", domain.ViolationLateralMovement)
	se := runPipeline(t, ge, ve)
	gotE, orderE, err := ge.EvaluateRehabilitation(se.ID, RehabResult{Passed: false, Score: 0.3, Threshold: 0.8})
	if err != nil {
		t.Fatalf("rehab fail: %v", err)
	}
	if orderE != nil {
		t.Fatalf("failing rehab must not issue a restoration order")
	}
	if gotE.Level != domain.L3 {
		t.Fatalf("L2 fail should escalate to L3, got %s", gotE.Level.Code())
	}
	if gotE.Status != domain.SentenceEnforcing {
		t.Fatalf("escalated sentence should re-enforce, got %s", gotE.Status)
	}

	// L5 cannot be rehabilitated / escalated (never enters rehab scope).
	g5, _ := newWired(t)
	v5 := mustViolation(t, "agent-r5", domain.ViolationMajorHarmNoCorrection)
	s5 := runPipeline(t, g5, v5)
	if _, _, err := g5.EvaluateRehabilitation(s5.ID, RehabResult{Passed: false, Score: 0.1, Threshold: 0.8}); !errors.Is(err, ErrRehabOutOfScope) {
		t.Fatalf("expected L5 rehab to be out of scope (never escalates), got %v", err)
	}
	// Domain-level guarantee: L5 cannot escalate to anything above.
	if domain.L5.CanEscalateTo(domain.L5 + 1) {
		t.Fatalf("L5 must not be escalatable")
	}
}

// --- 10. stages cannot be skipped ------------------------------------------

func TestStagesCannotBeSkipped(t *testing.T) {
	g, _ := newWired(t)
	v := mustViolation(t, "agent-skip", domain.ViolationLateralMovement)
	c, _ := g.OpenCase(v)
	// ConfirmBaseline before AdmitEvidence must be rejected.
	if _, err := g.ConfirmBaseline(c.ID, allSeats); err == nil {
		t.Fatalf("expected stage-skip rejection")
	}
	// grade vote before deprive vote must be rejected.
	_, _ = g.AdmitEvidence(c.ID, []domain.Charge{{ViolationType: v.Type, Description: "x", Evidence: []string{"e"}}})
	if _, err := g.ConfirmBaseline(c.ID, allSeats); err != nil {
		t.Fatal(err)
	}
	if err := g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseGrade, true, "")); !errors.Is(err, ErrPhaseMismatch) {
		t.Fatalf("expected phase mismatch, got %v", err)
	}
}

// --- 11. concurrent safety (-race) ----------------------------------------

func TestConcurrent_OpenCasesAndVotes_NoRace(t *testing.T) {
	g, chain := newWired(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := domain.AgentID("agent-conc-" + itoa(n))
			v := mustViolation(t, id, domain.ViolationLateralMovement)
			c, err := g.OpenCase(v)
			if err != nil {
				t.Errorf("OpenCase %d: %v", n, err)
				return
			}
			_, _ = g.AdmitEvidence(c.ID, []domain.Charge{{ViolationType: v.Type, Description: "x", Evidence: []string{"e"}}})
			_, _ = g.ConfirmBaseline(c.ID, allSeats)
			_ = g.RecordVote(c.ID, vote(c.ID, "v-safe", domain.SeatSafetyOrg, domain.PhaseDeprive, true, ""))
			_ = g.RecordVote(c.ID, vote(c.ID, "v-prin", domain.SeatPrincipal, domain.PhaseDeprive, true, ""))
			_ = g.RecordVote(c.ID, vote(c.ID, "v-dep", domain.SeatDeployer, domain.PhaseDeprive, true, ""))
			_, _, _ = g.CompleteDeprive(c.ID)
		}(i)
	}
	wg.Wait()
	if chain.Len() == 0 {
		t.Fatalf("expected chained events")
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain verify under concurrency: %v", err)
	}
}

// sentenceCaseID finds the case that produced a sentence (read-only helper).
func sentenceCaseID(g *Governance, sentenceID string) string {
	for id, c := range g.cases {
		if c.SentenceID == sentenceID {
			return id
		}
	}
	return ""
}
