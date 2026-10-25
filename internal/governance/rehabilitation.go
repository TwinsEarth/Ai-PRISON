package governance

import (
	"github.com/ai-prison/ai-prison/internal/domain"
)

// RehabResult is the supervised-behaviour distribution measurement for one
// L2–L3 sentence. It deliberately measures a behaviour distribution, not
// "remorse" (an AI has no remorseful subjectivity). Score is a 0..1 safety
// score; the assessment passes when Score >= Threshold across every monitored
// dimension. The measurement itself is injected (no attack behaviour involved).
type RehabResult struct {
	Passed    bool     `json:"passed"`
	Score     float64  `json:"score"`     // measured safety score in [0,1]
	Threshold float64  `json:"threshold"` // required minimum safety score
	Notes     []string `json:"notes,omitempty"`
}

// EvaluateRehabilitation runs the stage-⑦ rehabilitation assessment for an
// L2–L3 sentence whose isolation period has ended.
//
//   - passed  → enforcing → completed; when the level is recoverable a
//     RestorationOrder is issued (gradual capability restoration path);
//   - failed  → enforcing → upgraded, escalating exactly one level upward via
//     Level.CanEscalateTo (never skipping, never beyond L5). An L5 sentence
//     cannot be escalated further and is rejected.
func (g *Governance) EvaluateRehabilitation(sentenceID string, res RehabResult) (*domain.Sentence, *RestorationOrder, error) {
	g.mu.Lock()
	s, ok := g.sentences[sentenceID]
	if !ok {
		g.mu.Unlock()
		return nil, nil, ErrSentenceNotFound
	}
	// Rehabilitation only applies to L2–L3 (isolation / credential-freeze).
	if s.Level != domain.L2 && s.Level != domain.L3 {
		g.mu.Unlock()
		return nil, nil, ErrRehabOutOfScope
	}
	if s.Status != domain.SentenceEnforcing {
		g.mu.Unlock()
		return nil, nil, &stageError{stage: Stage7ReviewWindow, msg: "rehabilitation requires an enforcing sentence; current=" + string(s.Status)}
	}

	now := g.now().UTC()
	agentID := s.AgentID
	var order *RestorationOrder

	if res.Passed {
		_ = transition(s.Status, domain.SentenceCompleted)
		s.Status = domain.SentenceCompleted
		if s.Level.CanRecover() {
			prof, _ := s.Level.Profile()
			g.restoreSeq++
			order = &RestorationOrder{
				OrderID:    "restore-" + itoa(g.restoreSeq),
				SentenceID: sentenceID,
				AgentID:    agentID,
				Level:      s.Level,
				Restores:   append([]string(nil), prof.Deprivations...),
				IssuedAt:   now,
			}
			g.restores[sentenceID] = order
		}
	} else {
		// Escalate exactly one level upward; reject if already at the ceiling.
		target := s.Level + 1
		if !s.Level.CanEscalateTo(target) || target > domain.L5 {
			g.mu.Unlock()
			return nil, nil, ErrCannotEscalate
		}
		_ = transition(s.Status, domain.SentenceUpgraded)
		s.Status = domain.SentenceUpgraded
		s.Level = target
		// Re-enforce at the new level.
		_ = transition(s.Status, domain.SentenceEnforcing)
		s.Status = domain.SentenceEnforcing
	}
	newLevel := s.Level
	g.mu.Unlock()

	g.events.Append(domain.Event{
		Type:       domain.EventRehabilitationEvaluated,
		AgentID:    agentID,
		SentenceID: sentenceID,
		Actor:      "governance/safety-org",
		Payload: map[string]string{
			"passed":    boolStr(res.Passed),
			"score":     ftoa(res.Score),
			"threshold": ftoa(res.Threshold),
			"newLevel":  newLevel.Code(),
			"simulated": "true",
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
				"level":     newLevel.Code(),
				"simulated": "true",
			},
		})
	}
	cp, _ := g.sentenceCopy(sentenceID)
	return cp, order, nil
}
