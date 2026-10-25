package domain

import "time"

// ReputationEvent records a single reputation change. Reputation and the
// collaboration relationships it unlocks are the most valuable asset in the
// agent economy; L5 zeroes them and makes them non-transferable.
type ReputationEvent struct {
	AgentID    AgentID   `json:"agentId"`
	Delta      int       `json:"delta"`
	Reason     string    `json:"reason"`
	RecordedAt time.Time `json:"recordedAt"`
}

// ApplyReputation applies a level's reputation effect to a score, clamped at
// zero (reputation never goes negative; L5 resets it to zero).
func ApplyReputation(score int, level Level) (int, error) {
	p, err := level.Profile()
	if err != nil {
		return score, err
	}
	if p.ZeroReputation {
		return 0, nil
	}
	next := score + p.ReputationDelta
	if next < 0 {
		next = 0
	}
	return next, nil
}
