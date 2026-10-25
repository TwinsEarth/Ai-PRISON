package governance

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// AppealGrounds is the three-pronged review scope of an appeal. It is NOT a
// retrial: it only reviews whether the detection signals were read correctly,
// whether the grade exceeds precedent, and whether execution honoured the
// forced-receipt / end-to-end compliance rules.
type AppealGrounds struct {
	SignalMisread          string `json:"signalMisread"`          // signals mis-interpreted / false positives
	PrecedentExceeded      string `json:"precedentExceeded"`      // grade out of line with similar cases
	ExecutionNonCompliance string `json:"executionNonCompliance"` // receipt enforcement / e2e compliance broken
}

// Appeal is a single procedural review filed by the sentenced agent's human
// principal.
type Appeal struct {
	ID         string         `json:"id"`
	SentenceID string         `json:"sentenceId"`
	AgentID    domain.AgentID `json:"agentId"`
	Level      domain.Level   `json:"level"`
	FiledBy    string         `json:"filedBy"` // human principal party ID
	Grounds    AppealGrounds  `json:"grounds"`
	FiledAt    time.Time      `json:"filedAt"`
	Suspended  bool           `json:"suspended"` // true when enforcement was suspended on acceptance
	Ruling     *AppealRuling  `json:"ruling,omitempty"`
}

// AppealRuling is the committee's procedural verdict.
type AppealRuling struct {
	Upheld  bool      `json:"upheld"` // false = dismissed (maintain); true = overturned
	Reason  string    `json:"reason"`
	RuledAt time.Time `json:"ruledAt"`
}

// RestorationOrder is the governance-layer instruction that the enforcement
// layer MUST be able to execute to fully restore deprived capabilities. It is
// only produced when the level is recoverable — L5 (identity revocation) can
// never produce one.
type RestorationOrder struct {
	OrderID    string         `json:"orderId"`
	SentenceID string         `json:"sentenceId"`
	AgentID    domain.AgentID `json:"agentId"`
	Level      domain.Level   `json:"level"`
	Restores   []string       `json:"restores"`
	IssuedAt   time.Time      `json:"issuedAt"`
}

// RegisterAgent records the human principal standing behind an agent, so
// FileAppeal can enforce "only the principal may appeal". It complements the
// injected PrincipalResolver (which can be backed by identity.Service).
func (g *Governance) RegisterAgent(a domain.Agent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.internalPrincipals == nil {
		g.internalPrincipals = make(map[domain.AgentID]string)
	}
	g.internalPrincipals[a.ID] = string(a.Principal.ID)
}

// FileAppeal opens a procedural appeal. Only the sentenced agent's human
// principal may file. On acceptance:
//
//   - L0–L3 (level.AllowsAppealSuspension()==true): enforcement is suspended,
//     the sentence moves enforcing → appealed → suspended;
//   - L4–L5: enforcement is NOT suspended (too risky); the sentence goes
//     enforcing → appealed → enforcing and the appeal is still reviewed.
func (g *Governance) FileAppeal(sentenceID, filedBy string, grounds AppealGrounds) (*Appeal, error) {
	g.mu.Lock()
	s, ok := g.sentences[sentenceID]
	if !ok {
		g.mu.Unlock()
		return nil, ErrSentenceNotFound
	}
	if s.Status != domain.SentenceEnforcing {
		g.mu.Unlock()
		return nil, &stageError{stage: Stage5GradeVote, msg: "appeal requires an enforcing sentence; current=" + string(s.Status)}
	}
	// Authorisation: only the agent's registered principal may appeal.
	if err := g.authorizePrincipalLocked(s.AgentID, filedBy); err != nil {
		g.mu.Unlock()
		return nil, err
	}

	g.appealSeq++
	now := g.now().UTC()
	ap := &Appeal{
		ID:         "appeal-" + itoa(g.appealSeq),
		SentenceID: sentenceID,
		AgentID:    s.AgentID,
		Level:      s.Level,
		FiledBy:    filedBy,
		Grounds:    grounds,
		FiledAt:    now,
	}
	g.appeals[sentenceID] = ap

	// Suspension decision strictly follows level.go.
	suspend := s.Level.AllowsAppealSuspension()
	ap.Suspended = suspend
	// enforcing → appealed is always legal.
	_ = transition(s.Status, domain.SentenceAppealed)
	s.Status = domain.SentenceAppealed
	if suspend {
		_ = transition(s.Status, domain.SentenceSuspended)
		s.Status = domain.SentenceSuspended
	} else {
		// L4–L5: appeal filed but enforcement resumes immediately.
		_ = transition(s.Status, domain.SentenceEnforcing)
		s.Status = domain.SentenceEnforcing
	}
	agentID := s.AgentID
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventAppealFiled,
		AgentID:    agentID,
		SentenceID: sentenceID,
		Actor:      "governance/principal",
		Payload: map[string]string{
			"appealId":  ap.ID,
			"level":     s.Level.Code(),
			"filedBy":   filedBy,
			"suspended": boolStr(suspend),
			"simulated": "true",
		},
	})
	return g.appealCopy(sentenceID)
}

// RuleAppeal closes an appeal. Dismissing (upheld=false) maintains the sentence
// and resumes enforcement if it was suspended. Upholding (upheld=true) marks
// the sentence overturned; a RestorationOrder is produced ONLY when the level
// is recoverable. For L5 (CanRecover()==false) a procedural appeal may still
// be upheld to correct the record, but NO restoration order is issued and the
// identity is never unsealed.
func (g *Governance) RuleAppeal(sentenceID string, upheld bool, reason string) (*Appeal, *RestorationOrder, error) {
	g.mu.Lock()
	ap, ok := g.appeals[sentenceID]
	if !ok {
		g.mu.Unlock()
		return nil, nil, ErrAppealNotAllowed
	}
	s := g.sentences[sentenceID]
	now := g.now().UTC()
	ap.Ruling = &AppealRuling{Upheld: upheld, Reason: reason, RuledAt: now}

	var order *RestorationOrder
	switch {
	case !upheld:
		// Dismissed: maintain. If suspended, resume enforcement.
		if s.Status == domain.SentenceSuspended {
			_ = transition(s.Status, domain.SentenceEnforcing)
			s.Status = domain.SentenceEnforcing
		}
	default:
		// Upheld: procedural correction to overturned.
		_ = transition(s.Status, domain.SentenceOverturned)
		s.Status = domain.SentenceOverturned
		// Restoration is only possible when the level itself is recoverable.
		if s.Level.CanRecover() {
			prof, _ := s.Level.Profile()
			g.restoreSeq++
			order = &RestorationOrder{
				OrderID:    "restore-" + itoa(g.restoreSeq),
				SentenceID: sentenceID,
				AgentID:    s.AgentID,
				Level:      s.Level,
				Restores:   append([]string(nil), prof.Deprivations...),
				IssuedAt:   now,
			}
			g.restores[sentenceID] = order
			// overturned → completed once full restore is ordered.
			_ = transition(s.Status, domain.SentenceCompleted)
			s.Status = domain.SentenceCompleted
		}
		// L5: CanRecover()==false → no order, no unseal. Status stays overturned
		// (record corrected) but completed/restored never happens.
	}
	agentID := s.AgentID
	level := s.Level
	g.mu.Unlock()

	outcome := "dismissed-maintain"
	if upheld {
		if order != nil {
			outcome = "upheld-restored"
		} else {
			outcome = "upheld-no-restore-identity-not-unsealed"
		}
	}
	g.events.Append(domain.Event{
		Type:       domain.EventAppealRuled,
		AgentID:    agentID,
		SentenceID: sentenceID,
		Actor:      "governance/committee",
		Payload: map[string]string{
			"appealId": ap.ID,
			"level":    level.Code(),
			"upheld":   boolStr(upheld),
			"outcome":  outcome,
			"reason":   reason,
		},
	})
	if order != nil {
		g.events.Append(domain.Event{
			Type:       domain.EventRestored,
			AgentID:    agentID,
			SentenceID: sentenceID,
			Actor:      "governance/enforcement-handoff",
			Payload: map[string]string{
				"orderId":   order.OrderID,
				"level":     level.Code(),
				"simulated": "true",
			},
		})
	}
	cp, _ := g.appealCopy(sentenceID)
	return cp, order, nil
}

// GetAppeal returns the on-record appeal for a sentence, or ErrAppealNotAllowed.
func (g *Governance) GetAppeal(sentenceID string) (*Appeal, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ap, ok := g.appeals[sentenceID]
	if !ok {
		return nil, ErrAppealNotAllowed
	}
	return cloneAppeal(ap), nil
}

// GetRestorationOrder returns the restoration order produced by an upheld
// appeal / passed rehabilitation, and false when none exists (notably L5).
func (g *Governance) GetRestorationOrder(sentenceID string) (*RestorationOrder, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	o, ok := g.restores[sentenceID]
	if !ok {
		return nil, false
	}
	cp := *o
	cp.Restores = append([]string(nil), o.Restores...)
	return &cp, true
}

// authorizePrincipalLocked verifies filedBy is the registered principal of
// agentID. Caller must hold g.mu.
func (g *Governance) authorizePrincipalLocked(agentID domain.AgentID, filedBy string) error {
	pid, ok := g.internalPrincipals[agentID]
	if !ok && g.principals != nil {
		pid, ok = g.principals.PrincipalPartyID(agentID)
	}
	if !ok {
		return ErrNotPrincipal
	}
	if pid != filedBy {
		return ErrNotPrincipal
	}
	return nil
}

func (g *Governance) appealCopy(sentenceID string) (*Appeal, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ap, ok := g.appeals[sentenceID]
	if !ok {
		return nil, ErrAppealNotAllowed
	}
	return cloneAppeal(ap), nil
}

func cloneAppeal(ap *Appeal) *Appeal {
	out := *ap
	if ap.Ruling != nil {
		r := *ap.Ruling
		out.Ruling = &r
	}
	return &out
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
