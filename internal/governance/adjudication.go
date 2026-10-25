package governance

import (
	"github.com/ai-prison/ai-prison/internal/domain"
)

// mandatorySeats are the four committee seats that MUST be constituted before
// either consent vote (committee-charter.md §2). affected_node is optional and
// only added when the harm involves cross-network collaboration.
var mandatorySeats = []domain.CommitteeRole{
	domain.SeatSafetyOrg,
	domain.SeatPrincipal,
	domain.SeatDeployer,
	domain.SeatModelProvider,
}

// AdmitEvidence completes stage ② (证据与先例审查): the committee accepts the
// proven charges and supporting evidence hashes. It refuses an empty charge
// list. On success the case advances to stage ③ (grading baseline).
func (g *Governance) AdmitEvidence(caseID string, charges []domain.Charge) (*Case, error) {
	g.mu.Lock()
	c, err := g.mustCaseAt(caseID, Stage1Intake)
	if err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if len(charges) == 0 {
		g.mu.Unlock()
		return nil, ErrInsufficientEvidence
	}
	// Each charge must carry at least one supporting evidence pointer.
	for i, ch := range charges {
		if len(ch.Evidence) == 0 {
			g.mu.Unlock()
			return nil, &stageError{stage: Stage2Evidence, msg: "charge has no evidence pointer", idx: i}
		}
	}
	c.Charges = append([]domain.Charge(nil), charges...)
	c.Status = CaseAdjudicating
	c.Stage = Stage3Baseline
	agentID := c.Violation.AgentID
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventAdjudicated,
		AgentID:    agentID,
		SentenceID: "",
		Actor:      "governance/committee",
		Payload: map[string]string{
			"caseId":      caseID,
			"stage":       Stage2Evidence.Name(),
			"outcome":     "evidence-admitted",
			"chargeCount": itoa(len(charges)),
			"simulated":   "true",
		},
	})
	return g.caseCopy(caseID)
}

// ConfirmBaseline completes stage ③ (分级基线确认 + 管辖权与委员会组建): the
// recommended baseline level is confirmed from the violation table and the
// five-seat committee is formed. The four mandatory seats must all be present;
// affected_node is accepted when the harm spans network collaboration.
func (g *Governance) ConfirmBaseline(caseID string, seats []domain.CommitteeRole) (*Case, error) {
	g.mu.Lock()
	c, err := g.mustCaseAt(caseID, Stage3Baseline)
	if err != nil {
		g.mu.Unlock()
		return nil, err
	}
	if err := checkMandatorySeats(seats); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	c.Seats = append([]domain.CommitteeRole(nil), seats...)
	c.Stage = Stage4DepriveVote
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventAdjudicated,
		AgentID:    c.Violation.AgentID,
		SentenceID: "",
		Actor:      "governance/committee",
		Payload: map[string]string{
			"caseId":    caseID,
			"stage":     Stage3Baseline.Name(),
			"baseline":  c.Recommended.Code(),
			"seatCount": itoa(len(seats)),
			"simulated": "true",
		},
	})
	return g.caseCopy(caseID)
}

// RecordVote appends one committee vote to the immutable vote book. The vote
// phase must match the current voting stage (deprive at ④, grade at ⑤). There
// is deliberately NO method to edit or delete a recorded vote: dissenting
// votes and their Reason are append-only and survive for the whole chain.
func (g *Governance) RecordVote(caseID string, v domain.Vote) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, err := g.mustCaseAt(caseID, 0)
	if err != nil {
		return err
	}
	switch c.Stage {
	case Stage4DepriveVote:
		if v.Phase != domain.PhaseDeprive {
			return ErrPhaseMismatch
		}
	case Stage5GradeVote:
		if v.Phase != domain.PhaseGrade {
			return ErrPhaseMismatch
		}
	default:
		return ErrStageOutOfOrder
	}
	if v.RecordedAt.IsZero() {
		v.RecordedAt = g.now().UTC()
	}
	c.Votes = append(c.Votes, v)
	return nil
}

// CompleteDeprive closes stage ④ (第一阶段同意, phase=deprive). It tallies the
// deprive votes with the strict-majority rule in domain.TallyPhase:
//
//   - not passed → the case is dismissed (CaseDismissed), NO deprivation
//     sentence is produced, and a dismissal event is written to the chain;
//   - passed → the case advances to stage ⑤ (grade vote).
func (g *Governance) CompleteDeprive(caseID string) (*domain.ConsensusResult, *Case, error) {
	g.mu.Lock()
	c, err := g.mustCaseAt(caseID, Stage4DepriveVote)
	if err != nil {
		g.mu.Unlock()
		return nil, nil, err
	}
	tally := domain.TallyPhase(c.Votes, domain.PhaseDeprive)
	if !tally.Passed {
		now := g.now().UTC()
		c.Status = CaseDismissed
		c.Stage = StagePipelineDone
		c.ClosedAt = &now
		agentID := c.Violation.AgentID
		g.mu.Unlock()

		g.events.Append(domain.Event{
			Type:       domain.EventAdjudicated,
			AgentID:    agentID,
			SentenceID: "",
			Actor:      "governance/committee",
			Payload: map[string]string{
				"caseId":    caseID,
				"stage":     Stage4DepriveVote.Name(),
				"outcome":   "dismissed",
				"for":       itoa(tally.For),
				"against":   itoa(tally.Against),
				"simulated": "true",
			},
		})
		cp, _ := g.caseCopy(caseID)
		return &tally, cp, nil
	}
	c.Stage = Stage5GradeVote
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventAdjudicated,
		AgentID:    c.Violation.AgentID,
		SentenceID: "",
		Actor:      "governance/committee",
		Payload: map[string]string{
			"caseId":    caseID,
			"stage":     Stage4DepriveVote.Name(),
			"outcome":   "deprive-approved",
			"for":       itoa(tally.For),
			"against":   itoa(tally.Against),
			"simulated": "true",
		},
	})
	cp, _ := g.caseCopy(caseID)
	return &tally, cp, nil
}

// CompleteGrade closes stage ⑤ (第二阶段同意, phase=grade). The grade on the
// table is the confirmed baseline level (stage ③); members vote approve/against
// that grade. A strict majority forms the sentence level; no majority leaves
// the case deferred (CaseDeferred) with NO definite-level sentence.
func (g *Governance) CompleteGrade(caseID string) (*domain.ConsensusResult, *Case, error) {
	g.mu.Lock()
	c, err := g.mustCaseAt(caseID, Stage5GradeVote)
	if err != nil {
		g.mu.Unlock()
		return nil, nil, err
	}
	tally := domain.TallyPhase(c.Votes, domain.PhaseGrade)
	if !tally.Passed {
		now := g.now().UTC()
		c.Status = CaseDeferred
		c.Stage = StagePipelineDone
		c.ClosedAt = &now
		agentID := c.Violation.AgentID
		g.mu.Unlock()

		g.events.Append(domain.Event{
			Type:       domain.EventAdjudicated,
			AgentID:    agentID,
			SentenceID: "",
			Actor:      "governance/committee",
			Payload: map[string]string{
				"caseId":    caseID,
				"stage":     Stage5GradeVote.Name(),
				"outcome":   "deferred-no-majority-grade",
				"for":       itoa(tally.For),
				"against":   itoa(tally.Against),
				"simulated": "true",
			},
		})
		cp, _ := g.caseCopy(caseID)
		return &tally, cp, nil
	}
	c.Stage = Stage6Sentence
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventAdjudicated,
		AgentID:    c.Violation.AgentID,
		SentenceID: "",
		Actor:      "governance/committee",
		Payload: map[string]string{
			"caseId":    caseID,
			"stage":     Stage5GradeVote.Name(),
			"outcome":   "grade-approved",
			"grade":     c.Recommended.Code(),
			"for":       itoa(tally.For),
			"against":   itoa(tally.Against),
			"simulated": "true",
		},
	})
	cp, _ := g.caseCopy(caseID)
	return &tally, cp, nil
}

// mustCaseAt returns the case locked by the caller, verifying it exists and
// (when want != 0) sits on exactly the expected stage. It exists to make
// skipping a stage a hard error.
func (g *Governance) mustCaseAt(caseID string, want AdjudicationStage) (*Case, error) {
	c, ok := g.cases[caseID]
	if !ok {
		return nil, ErrCaseNotFound
	}
	if want != 0 && c.Stage != want {
		return nil, &stageError{stage: c.Stage, msg: "expected " + want.Name(), want: want}
	}
	return c, nil
}

// checkMandatorySeats ensures all four charter-mandated seats are present.
func checkMandatorySeats(seats []domain.CommitteeRole) error {
	have := make(map[domain.CommitteeRole]bool, len(seats))
	for _, s := range seats {
		have[s] = true
	}
	for _, m := range mandatorySeats {
		if !have[m] {
			return ErrCommitteeIncomplete
		}
	}
	return nil
}

// stageError annotates which stage a pipeline error occurred on.
type stageError struct {
	stage AdjudicationStage
	msg   string
	want  AdjudicationStage
	idx   int
}

func (e *stageError) Error() string {
	if e.want != 0 {
		return "governance: stage " + e.stage.Name() + " but " + e.msg
	}
	return "governance: stage " + e.stage.Name() + ": " + e.msg
}
