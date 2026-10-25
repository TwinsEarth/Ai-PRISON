package domain

import "time"

// SentenceStatus enumerates the lifecycle states of a disposition.
type SentenceStatus string

const (
	SentenceProposed   SentenceStatus = "proposed"   // drafted, awaiting committee vote
	SentenceApproved   SentenceStatus = "approved"   // both consent phases passed
	SentenceEnforcing  SentenceStatus = "enforcing"  // enforcement underway
	SentenceAppealed   SentenceStatus = "appealed"   // an appeal has been filed
	SentenceSuspended  SentenceStatus = "suspended"  // suspended pending appeal (L0–L3 only)
	SentenceCompleted  SentenceStatus = "completed"  // disposition served / rehabilitation passed
	SentenceUpgraded   SentenceStatus = "upgraded"   // rehabilitation failed, level raised
	SentenceOverturned SentenceStatus = "overturned" // appeal upheld, capabilities restored
)

// Charge is a single proven charge attached to a sentence.
type Charge struct {
	ViolationType ViolationType `json:"violationType"`
	Description   string        `json:"description"`
	Evidence      []string      `json:"evidence"` // event/hashes that prove the charge
}

// VotePhase implements the two-phase consent design:
// the committee first votes on whether to deprive at all, then on the grade.
type VotePhase string

const (
	PhaseDeprive VotePhase = "deprive" // phase 1: should capability be deprived?
	PhaseGrade   VotePhase = "grade"   // phase 2: to which level?
)

// CommitteeRole enumerates the committee seats.
type CommitteeRole string

const (
	SeatSafetyOrg     CommitteeRole = "agent_safety_org"    // Agent security organisation
	SeatPrincipal     CommitteeRole = "principal"           // the agent's human principal
	SeatDeployer      CommitteeRole = "deployer_compliance" // deployer compliance representative
	SeatModelProvider CommitteeRole = "model_provider_security"
	SeatAffectedNode  CommitteeRole = "affected_node" // representatives of affected network nodes
)

// Vote records one committee member's decision. Dissenting votes and their
// reasoning MUST be recorded and never deleted.
type Vote struct {
	VoterID    string        `json:"voterId"`
	Role       CommitteeRole `json:"role"`
	Phase      VotePhase     `json:"phase"`
	Approve    bool          `json:"approve"`
	Reason     string        `json:"reason"`
	RecordedAt time.Time     `json:"recordedAt"`
}

// Sentence is the adjudicated disposition for one agent.
type Sentence struct {
	ID             string                 `json:"id"`
	AgentID        AgentID                `json:"agentId"`
	DID            DID                    `json:"did"`
	Level          Level                  `json:"level"`
	Status         SentenceStatus         `json:"status"`
	Charges        []Charge               `json:"charges"`
	Votes          []Vote                 `json:"votes"`
	Deprivations   []string               `json:"deprivations"`
	Accountability []AccountabilityRecord `json:"accountability"`

	IssuedAt    time.Time  `json:"issuedAt"`
	EffectiveAt time.Time  `json:"effectiveAt"`
	ReviewAt    *time.Time `json:"reviewAt,omitempty"`

	// Hash-chain anchors (populated by internal/audit in v0.3).
	PrevHash string `json:"prevHash,omitempty"`
	Hash     string `json:"hash,omitempty"`
}

// Profile returns the level profile for this sentence.
func (s Sentence) Profile() (LevelProfile, error) { return s.Level.Profile() }

// AppealSuspends returns whether filing an appeal suspends this sentence.
func (s Sentence) AppealSuspends() bool { return s.Level.AllowsAppealSuspension() }

// IsPermanent reports whether this sentence is permanent.
func (s Sentence) IsPermanent() bool { return s.Level.IsPermanent() }

// ConsensusResult summarises a two-phase committee tally.
type ConsensusResult struct {
	Phase           VotePhase `json:"phase"`
	For             int       `json:"for"`
	Against         int       `json:"against"`
	Passed          bool      `json:"passed"`
	DissentRecorded bool      `json:"dissentRecorded"`
}

// TallyPhase counts the votes for a single consent phase. A phase passes with
// a strict majority; any dissent is flagged for the immutable record.
func TallyPhase(votes []Vote, phase VotePhase) ConsensusResult {
	r := ConsensusResult{Phase: phase}
	for _, v := range votes {
		if v.Phase != phase {
			continue
		}
		if v.Approve {
			r.For++
		} else {
			r.Against++
			r.DissentRecorded = true
		}
	}
	r.Passed = r.For > r.Against && (r.For+r.Against) > 0
	return r
}
