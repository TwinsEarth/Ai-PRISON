package governance

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// CaseStatus is the lifecycle of an adjudicated case (立案案件). A case is
// NOT a sentence: a detected violation only opens a case, and most of the
// governance decision happens before any sentence exists.
type CaseStatus string

const (
	// CaseOpen: 已立案 / 待裁定 — the violation was accepted as a filing basis.
	CaseOpen CaseStatus = "open"
	// CaseAdjudicating: the seven-stage pipeline is in progress.
	CaseAdjudicating CaseStatus = "adjudicating"
	// CaseDismissed: phase 1 (deprive) did not reach a strict majority; the
	// case is closed WITHOUT any deprivation sentence.
	CaseDismissed CaseStatus = "dismissed"
	// CaseDeferred: phase 1 passed but phase 2 (grade) reached no strict
	// majority, so no definite-level sentence can be formed.
	CaseDeferred CaseStatus = "deferred"
	// CaseSentenced: a sentence has been generated (status=approved).
	CaseSentenced CaseStatus = "sentenced"
)

// AdjudicationStage enumerates the seven ordered stages of the committee
// pipeline, aligned with docs/governance-templates/committee-charter.md §4.1.
// Stages cannot be skipped: each method that advances the pipeline verifies
// the case is sitting exactly on the prior stage.
type AdjudicationStage int

const (
	// StageNone is the zero value (no case).
	StageNone AdjudicationStage = iota
	// Stage1Intake ① 立案与信号核验: signal integrity accepted.
	Stage1Intake
	// Stage2Evidence ② 证据与先例审查: charges/evidence assembled.
	Stage2Evidence
	// Stage3Baseline ③ 分级基线确认: recommended level + committee seats.
	Stage3Baseline
	// Stage4DepriveVote ④ 第一阶段表决(deprive): votes in phase=deprive.
	Stage4DepriveVote
	// Stage5GradeVote ⑤ 第二阶段表决(grade): votes in phase=grade.
	Stage5GradeVote
	// Stage6Sentence ⑥ 异议记录与判决生成: dissent recorded + sentence issued.
	Stage6Sentence
	// Stage7ReviewWindow ⑦ 申诉/矫正复核窗口: appeal/rehab window disclosed.
	Stage7ReviewWindow
	// StagePipelineDone is the terminal marker.
	StagePipelineDone
)

// Name renders the stage as its canonical Chinese label used in the charter.
func (s AdjudicationStage) Name() string {
	switch s {
	case Stage1Intake:
		return "①立案与信号核验"
	case Stage2Evidence:
		return "②证据与先例审查"
	case Stage3Baseline:
		return "③分级基线确认"
	case Stage4DepriveVote:
		return "④第一阶段表决(deprive)"
	case Stage5GradeVote:
		return "⑤第二阶段表决(grade)"
	case Stage6Sentence:
		return "⑥异议记录与判决生成"
	case Stage7ReviewWindow:
		return "⑦申诉/矫正复核窗口"
	default:
		return "none"
	}
}

// Case is one adjudicated incident. Votes are append-only: this package stores
// them but exposes no edit/delete operation (the charter requires dissent to
// be immutable).
type Case struct {
	ID          string            `json:"id"`
	Violation   domain.Violation  `json:"violation"`
	Recommended domain.Level      `json:"recommendedLevel"`
	Status      CaseStatus        `json:"status"`
	Stage       AdjudicationStage `json:"stage"`
	Charges     []domain.Charge   `json:"charges"`
	// Seats is the committee roster constituted at stage ③ (the four mandatory
	// seats are always present; affected_node is added for cross-network harm).
	Seats []domain.CommitteeRole `json:"seats"`
	// Votes is the immutable committee vote book (deprive then grade).
	Votes      []domain.Vote `json:"votes"`
	SentenceID string        `json:"sentenceId,omitempty"`
	OpenedAt   time.Time     `json:"openedAt"`
	ClosedAt   *time.Time    `json:"closedAt,omitempty"`
}

// DissentVotes returns a read-only copy of every recorded vote that did not
// approve its phase. There is intentionally no method to remove them.
func (c *Case) DissentVotes() []domain.Vote {
	out := make([]domain.Vote, 0, len(c.Votes))
	for _, v := range c.Votes {
		if !v.Approve {
			out = append(out, v)
		}
	}
	return out
}

// Governance wires the injected sinks and runs the adjudication / state
// machine. It is fully concurrent-safe (one mutex guards all mutable state).
type Governance struct {
	mu         sync.RWMutex
	events     EventSink
	principals PrincipalResolver
	now        func() time.Time

	cases     map[string]*Case
	sentences map[string]*domain.Sentence
	appeals   map[string]*Appeal
	restores  map[string]*RestorationOrder
	// internalPrincipals is the fallback principal registry populated by
	// RegisterAgent, consulted before the injected PrincipalResolver.
	internalPrincipals map[domain.AgentID]string

	caseSeq    int
	sentSeq    int
	appealSeq  int
	restoreSeq int
}

// Config builds a Governance; nil fields fall back to safe no-op / defaults.
type Config struct {
	Events     EventSink
	Principals PrincipalResolver
	// Now overrides the clock; zero means time.Now().
	Now func() time.Time
}

// New returns a wired Governance.
func New(cfg Config) *Governance {
	g := &Governance{
		events:     cfg.Events,
		principals: cfg.Principals,
		now:        cfg.Now,
		cases:      make(map[string]*Case),
		sentences:  make(map[string]*domain.Sentence),
		appeals:    make(map[string]*Appeal),
		restores:   make(map[string]*RestorationOrder),
	}
	if g.events == nil {
		g.events = noopEventSink{}
	}
	if g.now == nil {
		g.now = time.Now
	}
	return g
}

// Errors returned by the governance layer. They are exported so callers (and
// tests) can discriminate failure modes.
var (
	ErrCaseNotFound         = errors.New("governance: case not found")
	ErrSentenceNotFound     = errors.New("governance: sentence not found")
	ErrStageOutOfOrder      = errors.New("governance: pipeline stage out of order (cannot skip stages)")
	ErrInvalidViolation     = errors.New("governance: unknown violation type or empty signals (signal integrity check failed)")
	ErrInsufficientEvidence = errors.New("governance: no charges/evidence supplied (stage ② evidence review)")
	ErrCommitteeIncomplete  = errors.New("governance: committee missing a mandatory seat (stage ③ jurisdiction/formation)")
	ErrPhaseMismatch        = errors.New("governance: vote phase does not match the current voting stage")
	ErrNoDissentGate        = errors.New("governance: internal tally error")
	ErrNoAccountability     = errors.New("governance: sentence refused — missing human accountability record (stage ⑥)")
	ErrIllegalTransition    = errors.New("governance: illegal sentence state transition")
	ErrNotPrincipal         = errors.New("governance: only the agent's human principal may file an appeal")
	ErrAppealNotAllowed     = errors.New("governance: no appeal on record for this sentence")
	ErrLevelNotRecoverable  = errors.New("governance: level cannot be recovered (no restoration order; identity not unsealed)")
	ErrRehabOutOfScope      = errors.New("governance: rehabilitation assessment only applies to L2–L3")
	ErrCannotEscalate       = errors.New("governance: cannot escalate beyond L5")
)

// OpenCase accepts a detected violation as a FILING BASIS only — it is not a
// sentence — and completes stage ① (intake / signal integrity): the violation
// type must be a known, defensively-named type and carry at least one raw
// signal. The case opens at status=open (已立案/待裁定) and the recommended
// baseline level is read from domain.Violation.RecommendedLevel.
func (g *Governance) OpenCase(v domain.Violation) (*Case, error) {
	if !domain.KnownViolationType(v.Type) || len(v.Signals) == 0 {
		return nil, ErrInvalidViolation
	}
	rec, ok := v.RecommendedLevel()
	if !ok {
		return nil, ErrInvalidViolation
	}
	g.mu.Lock()
	g.caseSeq++
	c := &Case{
		ID:          fmt.Sprintf("case-%d", g.caseSeq),
		Violation:   v,
		Recommended: rec,
		Status:      CaseOpen,
		Stage:       Stage1Intake,
		OpenedAt:    g.now().UTC(),
	}
	g.cases[c.ID] = c
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventViolationDetected,
		AgentID:    v.AgentID,
		SentenceID: "",
		Actor:      "governance/detector",
		Payload: map[string]string{
			"caseId":        c.ID,
			"violationType": string(v.Type),
			"recommended":   rec.Code(),
			"source":        v.Source,
			"simulated":     "true",
		},
	})
	return g.caseCopy(c.ID)
}

// GetCase returns a detached copy of the case, or ErrCaseNotFound.
func (g *Governance) GetCase(id string) (*Case, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.cases[id]
	if !ok {
		return nil, ErrCaseNotFound
	}
	return cloneCase(c), nil
}

// caseCopy returns a detached copy; must be called WITHOUT the lock.
func (g *Governance) caseCopy(id string) (*Case, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c, ok := g.cases[id]
	if !ok {
		return nil, ErrCaseNotFound
	}
	return cloneCase(c), nil
}

// cloneCase detaches the mutable slices so callers cannot alias internal state.
func cloneCase(c *Case) *Case {
	out := *c
	out.Charges = append([]domain.Charge(nil), c.Charges...)
	out.Seats = append([]domain.CommitteeRole(nil), c.Seats...)
	out.Votes = append([]domain.Vote(nil), c.Votes...)
	return &out
}
