package domain

import "time"

// HumanParty identifies a real, accountable legal entity that stands behind
// an agent. Because an LLM agent has no persistent "body" that can receive a
// punitive signal, the consequences of a disposition must be coupled to
// humans who can actually be constrained.
type HumanParty struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`    // human-readable role
	Contact string `json:"contact"` // controlled / minimal contact handle
}

// AccountableRole enumerates the three human-side roles defined in the design.
type AccountableRole string

const (
	RolePrincipal     AccountableRole = "principal"      // 人类委托人
	RoleDeployer      AccountableRole = "deployer"       // 部署方
	RoleModelProvider AccountableRole = "model_provider" // 模型提供方
)

// AccountabilityFinding records how responsibility is assigned on the human side.
type AccountabilityFinding string

const (
	// FindingJointLiability: the violation occurred within the scope the
	// principal authorised — the principal bears joint liability.
	FindingJointLiability AccountabilityFinding = "joint_liability"
	// FindingMonitoringDutyReview: the violation exceeded authorisation —
	// the deployer's monitoring duty is examined.
	FindingMonitoringDutyReview AccountabilityFinding = "monitoring_duty_review"
	// FindingCapabilityControlDutyReview: the violation exploited the model's
	// inherent dangerous knowledge — the provider's capability-control duty
	// is examined.
	FindingCapabilityControlDutyReview AccountabilityFinding = "capability_control_duty_review"
	// FindingNone: no human-side finding for this record.
	FindingNone AccountabilityFinding = "none"
)

// AccountabilityRecord couples an agent-side disposition to a human-side
// responsibility outcome. Every sentence must carry at least one such record.
type AccountabilityRecord struct {
	Party               HumanParty            `json:"party"`
	Role                AccountableRole       `json:"role"`
	Finding             AccountabilityFinding `json:"finding"`
	WithinAuthorization bool                  `json:"withinAuthorization"`
	Reason              string                `json:"reason"`
	RecordedAt          time.Time             `json:"recordedAt"`
}
