package identity

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// OpCommunicationCut is the simulated receipt operation for "this node has
// cut the agent's communication channel". It is an opaque receipt label; the
// audit compliance evaluator only special-cases the snapshot-seal op.
const OpCommunicationCut = "communication.cut"

// NetworkTopology models the simulated node ↔ agent placement. It is an
// interface on purpose: the shipped implementation is in-memory only
// (topology.go), and tests can substitute fakes. This is NOT a real cluster
// topology — there is no real node fleet behind it.
type NetworkTopology interface {
	// Register places agentID on nodeID. An agent may be replicated across
	// several nodes (each holds a copy of its communication channel).
	Register(nodeID string, agentID domain.AgentID)
	// Deregister removes every placement of agentID.
	Deregister(agentID domain.AgentID)
	// NodesHosting returns every node currently carrying agentID's channel,
	// sorted.
	NodesHosting(agentID domain.AgentID) []string
	// AgentsOn returns every agent whose channel is placed on nodeID, sorted.
	AgentsOn(nodeID string) []domain.AgentID
}

// RevocationSink is the write side of the global revocation set, injected so
// this package never hard-couples to internal/revocation.
type RevocationSink interface {
	// RevokeCredential writes one credential of the given kind into the global
	// revocation set. holdingNodes are the simulated nodes that must
	// acknowledge. Idempotent by credential ID on the real set.
	RevokeCredential(id string, kind domain.CredentialKind, agentID domain.AgentID, sentenceID, reason string, holdingNodes []string)
}

// EventSink is the append side of the judgement hash chain.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// ReceiptController is the forced-receipt side of the execution receipt book:
// broadcast a pending receipt, let a node acknowledge it, and ask the book
// which nodes are still untrusted. Untrusted judgment is deliberately owned
// by the audit receipt book (pending/missing = untrusted); this package does
// not keep a second un-acked set.
type ReceiptController interface {
	RegisterPending(r domain.Receipt) domain.Receipt
	Ack(receiptID, nodeKey string, now time.Time) (domain.Receipt, bool)
	UntrustedNodes() []string
}

// FreezeOrder is one simulated instruction to freeze an agent. GroupFreeze is
// true for collateral agents frozen because an untrusted node hosts them —
// not because they themselves were sentenced.
type FreezeOrder struct {
	AgentID     domain.AgentID `json:"agentId"`
	NodeID      string         `json:"nodeId"`
	SentenceID  string         `json:"sentenceId"`
	Reason      string         `json:"reason"`
	GroupFreeze bool           `json:"groupFreeze"` // collateral freeze along the untrusted node
}

// IdentityReport is the result of one RevokeIdentity call: the two revoked
// credentials (identity + communication), the nodes that had to acknowledge,
// and the human-side accountability record attached to the revocation.
type IdentityReport struct {
	AgentID            domain.AgentID                `json:"agentId"`
	SentenceID         string                        `json:"sentenceId"`
	RevokedCredentials []string                      `json:"revokedCredentials"` // identity then communication
	HoldingNodes       []string                      `json:"holdingNodes"`
	Accountability     []domain.AccountabilityRecord `json:"accountability"`
	CardRevoked        bool                          `json:"cardRevoked"`
}

// CutReport is the live projection of one communication cut: the nodes the cut
// was broadcast to, which of them acknowledged, which are still untrusted, and
// the freeze orders (collateral) that the untrusted nodes impose.
type CutReport struct {
	SentenceID        string         `json:"sentenceId"`
	AgentID           domain.AgentID `json:"agentId"`
	BroadcastNodes    []string       `json:"broadcastNodes"`
	AcknowledgedNodes []string       `json:"acknowledgedNodes"`
	UntrustedNodes    []string       `json:"untrustedNodes"`
	FrozenAgents      []FreezeOrder  `json:"frozenAgents"`
}
