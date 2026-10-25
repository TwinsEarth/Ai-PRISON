package governance

import (
	"fmt"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// legalTransitions is the authoritative sentence state machine, rendered from
// docs/04-governance.md §4. Every governance method that mutates a sentence
// status goes through transition(), so an illegal move (e.g. proposed →
// completed, or an attempt to "restore" an L5 identity) is rejected here.
var legalTransitions = map[domain.SentenceStatus]map[domain.SentenceStatus]bool{
	// drafted through the two-phase vote.
	domain.SentenceProposed: {
		domain.SentenceApproved: true,
	},
	// approved becomes enforcing at effectiveAt.
	domain.SentenceApproved: {
		domain.SentenceEnforcing: true,
	},
	// enforcing is the main working state: appeal, serve, rehab pass or rehab fail.
	domain.SentenceEnforcing: {
		domain.SentenceAppealed:   true,
		domain.SentenceCompleted:  true, // disposition served / rehabilitation passed
		domain.SentenceUpgraded:   true, // rehabilitation failed, level raised
		domain.SentenceOverturned: true, // appeal upheld on a level that never suspended (L4–L5)
	},
	// appeal filed: L0–L3 suspend, L4–L5 resume enforcement immediately.
	domain.SentenceAppealed: {
		domain.SentenceSuspended: true,
		domain.SentenceEnforcing: true, // L4–L5: filed but NOT suspended
	},
	// suspended pending review: resume on dismissal, overturn on uphold.
	domain.SentenceSuspended: {
		domain.SentenceEnforcing:  true, // appeal dismissed / rejected, resume
		domain.SentenceOverturned: true, // appeal upheld, full restore path
		domain.SentenceUpgraded:   true, // appeal rejected and rehab fails
	},
	// upgraded re-enforces at the new level.
	domain.SentenceUpgraded: {
		domain.SentenceEnforcing: true,
	},
	// overturned (procedural correction) restores recoverable capabilities.
	domain.SentenceOverturned: {
		domain.SentenceCompleted: true,
	},
	// completed is terminal.
	domain.SentenceCompleted: {},
}

// transition reports whether moving a sentence from→to is a legal edge of the
// state machine. It returns ErrIllegalTransition otherwise.
func transition(from, to domain.SentenceStatus) error {
	if from == to {
		return nil
	}
	allowed, ok := legalTransitions[from]
	if !ok || !allowed[to] {
		return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
	}
	return nil
}

// canReach reports whether to is reachable from from (read-only predicate for
// tests and diagnostics).
func canReach(from, to domain.SentenceStatus) bool {
	allowed, ok := legalTransitions[from]
	return ok && allowed[to]
}
