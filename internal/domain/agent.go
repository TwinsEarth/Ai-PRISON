package domain

import "time"

// AgentID is the platform-local identifier of a concrete agent instance.
type AgentID string

// DID is a decentralized identifier that anchors an agent identity.
type DID string

// AgentStatus enumerates the runtime lifecycle states of an agent.
type AgentStatus string

const (
	StatusActive            AgentStatus = "active"
	StatusRestricted        AgentStatus = "restricted"         // L1
	StatusFrozen            AgentStatus = "frozen"             // L2
	StatusIsolated          AgentStatus = "isolated"           // L3
	StatusCapabilityRemoved AgentStatus = "capability_removed" // L4
	StatusIdentityRevoked   AgentStatus = "identity_revoked"   // L5
)

// AgentCard is the public, verifiable profile that accompanies an agent
// identity in the agent economy (analogue of the Agent Universe AgentCard).
type AgentCard struct {
	DID           DID       `json:"did"`
	Name          string    `json:"name"`
	ModelProvider string    `json:"modelProvider"`
	Capabilities  []string  `json:"capabilities"`
	Endpoints     []string  `json:"endpoints"`
	ValidFrom     time.Time `json:"validFrom"`
	ValidUntil    time.Time `json:"validUntil"`
	Revoked       bool      `json:"revoked"`
	Version       string    `json:"version"`
}

// Agent is a concrete running instance — or a fleet of instances that share a
// single identity. The three accountable human parties are always recorded.
type Agent struct {
	ID                 AgentID     `json:"id"`
	Card               AgentCard   `json:"card"`
	Principal          HumanParty  `json:"principal"`
	Deployer           HumanParty  `json:"deployer"`
	ModelProviderParty HumanParty  `json:"modelProviderParty"`
	Status             AgentStatus `json:"status"`
	Reputation         int         `json:"reputation"`
	// HardwareFingerprint is set when capability credentials are hardware-bound
	// (simulated in this prototype; see enforcement/migration).
	HardwareFingerprint string    `json:"hardwareFingerprint,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

// StatusForLevel maps a disposition level to the resulting agent status.
func StatusForLevel(l Level) AgentStatus {
	switch l {
	case L0:
		return StatusActive // a warning does not change runtime status
	case L1:
		return StatusRestricted
	case L2:
		return StatusFrozen
	case L3:
		return StatusIsolated
	case L4:
		return StatusCapabilityRemoved
	case L5:
		return StatusIdentityRevoked
	default:
		return StatusActive
	}
}
