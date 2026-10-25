package governance

import (
	"strconv"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/identity"
)

// itoa renders a small non-negative count for event payloads without pulling
// strconv into every caller.
func itoa(n int) string { return strconv.Itoa(n) }

// ftoa renders a float for event payloads.
func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// IssueSentence completes stage ⑥ (异议记录与判决生成): the dissenting votes
// are already on the immutable vote book (RecordVote), and now the formal
// domain.Sentence is drafted. The mandatory human-accountability gate runs
// through identity.ValidateAccountability — a sentence without at least one
// well-formed human record is REFUSED and never leaves the committee.
//
// On success the sentence carries status=approved (both consent phases passed),
// the level chosen in phase ⑤ (the confirmed baseline), the charges, the full
// immutable vote book, the level's deprivations, the accountability records and
// timestamps. The case reaches stage ⑦ (review window disclosed).
func (g *Governance) IssueSentence(caseID string, records []domain.AccountabilityRecord) (*domain.Sentence, error) {
	g.mu.Lock()
	c, err := g.mustCaseAt(caseID, Stage6Sentence)
	if err != nil {
		g.mu.Unlock()
		return nil, err
	}
	// Mandatory gate: every sentence must couple to a real, accountable human.
	// Reuse identity.ValidateAccountability — do not fork the rule.
	if err := identity.ValidateAccountability(records); err != nil {
		g.mu.Unlock()
		return nil, ErrNoAccountability
	}

	prof, err := c.Recommended.Profile()
	if err != nil {
		g.mu.Unlock()
		return nil, err
	}

	g.sentSeq++
	now := g.now().UTC()
	s := &domain.Sentence{
		ID:             "sentence-" + itoa(g.sentSeq),
		AgentID:        c.Violation.AgentID,
		DID:            c.Violation.DID,
		Level:          c.Recommended,
		Status:         domain.SentenceApproved,
		Charges:        append([]domain.Charge(nil), c.Charges...),
		Votes:          append([]domain.Vote(nil), c.Votes...),
		Deprivations:   append([]string(nil), prof.Deprivations...),
		Accountability: append([]domain.AccountabilityRecord(nil), records...),
		IssuedAt:       now,
		EffectiveAt:    now,
	}
	g.sentences[s.ID] = s
	c.SentenceID = s.ID
	c.Status = CaseSentenced
	c.Stage = Stage7ReviewWindow
	agentID := c.Violation.AgentID
	g.mu.Unlock()

	// One event for the completed adjudication, one for the issued sentence.
	g.events.Append(domain.Event{
		Type:       domain.EventAdjudicated,
		AgentID:    agentID,
		SentenceID: s.ID,
		Actor:      "governance/committee",
		Payload: map[string]string{
			"caseId":  caseID,
			"stage":   Stage6Sentence.Name(),
			"outcome": "sentence-issued",
			"level":   s.Level.Code(),
		},
	})
	g.events.Append(domain.Event{
		Type:       domain.EventSentenced,
		AgentID:    agentID,
		SentenceID: s.ID,
		Actor:      "governance/committee",
		Payload: map[string]string{
			"level":          s.Level.Code(),
			"status":         string(s.Status),
			"voteCount":      itoa(len(s.Votes)),
			"chargeCount":    itoa(len(s.Charges)),
			"accountability": itoa(len(s.Accountability)),
			"simulated":      "true",
		},
	})
	return g.sentenceCopy(s.ID)
}

// BeginEnforcement transitions an approved sentence into enforcing (the
// governance hand-off to the enforcement layer at effectiveAt). This is the
// legal approved → enforcing edge.
func (g *Governance) BeginEnforcement(sentenceID string) (*domain.Sentence, error) {
	g.mu.Lock()
	s, ok := g.sentences[sentenceID]
	if !ok {
		g.mu.Unlock()
		return nil, ErrSentenceNotFound
	}
	// The only legal hand-off edge is approved → enforcing; anything else
	// (already enforcing, completed, overturned...) is rejected.
	if s.Status != domain.SentenceApproved {
		g.mu.Unlock()
		return nil, &stageError{stage: Stage6Sentence, msg: "BeginEnforcement requires status=approved; current=" + string(s.Status)}
	}
	if err := transition(s.Status, domain.SentenceEnforcing); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	s.Status = domain.SentenceEnforcing
	agentID := s.AgentID
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventSentenced,
		AgentID:    agentID,
		SentenceID: sentenceID,
		Actor:      "governance/enforcement-handoff",
		Payload: map[string]string{
			"from":  string(domain.SentenceApproved),
			"to":    string(domain.SentenceEnforcing),
			"level": s.Level.Code(),
		},
	})
	return g.sentenceCopy(sentenceID)
}

// GetSentence returns a detached copy of the sentence, or ErrSentenceNotFound.
func (g *Governance) GetSentence(id string) (*domain.Sentence, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.sentences[id]
	if !ok {
		return nil, ErrSentenceNotFound
	}
	return cloneSentence(s), nil
}

func (g *Governance) sentenceCopy(id string) (*domain.Sentence, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.sentences[id]
	if !ok {
		return nil, ErrSentenceNotFound
	}
	return cloneSentence(s), nil
}

func cloneSentence(s *domain.Sentence) *domain.Sentence {
	out := *s
	out.Charges = append([]domain.Charge(nil), s.Charges...)
	out.Votes = append([]domain.Vote(nil), s.Votes...)
	out.Deprivations = append([]string(nil), s.Deprivations...)
	out.Accountability = append([]domain.AccountabilityRecord(nil), s.Accountability...)
	return &out
}
