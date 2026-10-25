package identity

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// credential ID handle conventions. They are deterministic so callers (and the
// test that proves "revoked-but-not-expired") can build the exact
// domain.Credential that the real revocation set will reject.
func identityCredID(agentID domain.AgentID) string      { return "identity:" + string(agentID) }
func communicationCredID(agentID domain.AgentID) string { return "communication:" + string(agentID) }

// agentRecord is the mutable in-memory bookkeeping row for one registered
// agent. Simulated, not a real registry.
type agentRecord struct {
	Card      domain.AgentCard
	Principal domain.HumanParty
	Deployer  domain.HumanParty
	Provider  domain.HumanParty
	Status    domain.AgentStatus
}

// cutState remembers, for one sentence, which pending receipt was registered
// on which node, so AckNode can find it and the report can be scoped to this
// cut rather than the whole receipt book.
type cutState struct {
	sentenceID string
	agentID    domain.AgentID
	receipts   map[string]string // nodeID -> receiptID
}

// Service wires the injected sinks and runs the identity/communication
// orchestration. Dependencies are small interfaces (types.go) so tests can
// substitute fakes; the real adapters live in adapters.go.
type Service struct {
	mu          sync.RWMutex
	agents      map[domain.AgentID]*agentRecord
	cuts        map[string]*cutState
	topology    NetworkTopology
	revocations RevocationSink
	events      EventSink
	receipts    ReceiptController
	now         func() time.Time
}

// Config builds a Service; nil fields fall back to safe in-memory / no-op
// behaviour.
type Config struct {
	Topology    NetworkTopology
	Revocations RevocationSink
	Events      EventSink
	Receipts    ReceiptController
}

// NewService returns a wired Service.
func NewService(cfg Config) *Service {
	s := &Service{
		agents:      make(map[domain.AgentID]*agentRecord),
		cuts:        make(map[string]*cutState),
		topology:    cfg.Topology,
		revocations: cfg.Revocations,
		events:      cfg.Events,
		receipts:    cfg.Receipts,
		now:         time.Now,
	}
	if s.topology == nil {
		s.topology = NewInMemoryTopology()
	}
	if s.revocations == nil {
		s.revocations = noopRevocations{}
	}
	if s.events == nil {
		s.events = noopEvents{}
	}
	if s.receipts == nil {
		s.receipts = noopReceipts{}
	}
	return s
}

// RegisterAgent records an agent (its AgentCard and the three human parties)
// and places it on the given node IDs. It does NOT revoke anything.
func (s *Service) RegisterAgent(a domain.Agent, nodeIDs ...string) {
	s.mu.Lock()
	s.agents[a.ID] = &agentRecord{
		Card:      a.Card,
		Principal: a.Principal,
		Deployer:  a.Deployer,
		Provider:  a.ModelProviderParty,
		Status:    a.Status,
	}
	s.mu.Unlock()
	for _, n := range nodeIDs {
		s.topology.Register(n, a.ID)
	}
}

// GetAgent returns a detached copy of the registered agent's card/status.
func (s *Service) GetAgent(agentID domain.AgentID) (domain.Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.agents[agentID]
	if !ok {
		return domain.Agent{}, false
	}
	return domain.Agent{
		ID:                 agentID,
		Card:               rec.Card,
		Principal:          rec.Principal,
		Deployer:           rec.Deployer,
		ModelProviderParty: rec.Provider,
		Status:             rec.Status,
	}, true
}

// RevokeIdentity writes the agent's identity AND communication credentials
// into the global revocation set, flips AgentCard.Revoked, emits the
// EventIdentityRevoked event, and — the mandatory invariant — REFUSES the
// disposition unless at least one well-formed human accountability record
// travels with it.
//
// Idempotent at the revocation set: re-revoking the same agent writes no
// duplicate entries (the set dedupes by credential ID).
func (s *Service) RevokeIdentity(agentID domain.AgentID, sentenceID, reason string, records []domain.AccountabilityRecord) (*IdentityReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("identity: unknown agent %q", agentID)
	}
	// Mandatory invariant: a disposition without a human-side record is
	// rejected outright — agent-side bleeding-stop must be coupled to a
	// accountable human.
	if err := ValidateAccountability(records); err != nil {
		return nil, err
	}

	holding := s.topology.NodesHosting(agentID)
	identityID := identityCredID(agentID)
	commID := communicationCredID(agentID)

	// Both credential categories go into the global revocation set. The set
	// checks membership BEFORE expiry/state, so domain.Credential.IsUsable
	// will now reject them even inside their validity window.
	s.revocations.RevokeCredential(identityID, domain.CredentialIdentity, agentID, sentenceID, reason, holding)
	s.revocations.RevokeCredential(commID, domain.CredentialCommunication, agentID, sentenceID, reason, holding)

	rec.Card.Revoked = true
	rec.Status = domain.StatusIdentityRevoked

	s.events.Append(domain.Event{
		Type:       domain.EventIdentityRevoked,
		AgentID:    agentID,
		SentenceID: sentenceID,
		Actor:      "identity-layer",
		Payload: map[string]string{
			"identityCredential": identityID,
			"commCredential":     commID,
			"holdingNodes":       joinSorted(holding),
			"reason":             reason,
			"simulated":          "true",
		},
	})

	return &IdentityReport{
		AgentID:            agentID,
		SentenceID:         sentenceID,
		RevokedCredentials: []string{identityID, commID},
		HoldingNodes:       holding,
		Accountability:     records,
		CardRevoked:        rec.Card.Revoked,
	}, nil
}

// CutCommunication broadcasts a forced "channel cut" receipt to every node
// carrying the agent's communication channel and then projects the live group
// state: nodes that have not acknowledged are untrusted, and every agent on
// such a node — including collateral, innocent agents — receives a freeze
// order. "Broadcast" is deliberately not treated as "delivered".
func (s *Service) CutCommunication(agentID domain.AgentID, sentenceID, reason string) (*CutReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes := s.topology.NodesHosting(agentID)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("identity: agent %q is placed on no node; nothing to cut", agentID)
	}

	cs := &cutState{sentenceID: sentenceID, agentID: agentID, receipts: make(map[string]string, len(nodes))}
	for _, node := range nodes {
		receiptID := sentenceID + "|" + OpCommunicationCut + "|" + node
		s.receipts.RegisterPending(domain.Receipt{
			ID:          receiptID,
			Operation:   OpCommunicationCut,
			SentenceID:  sentenceID,
			NodeID:      node,
			BeforeState: "channel:open",
			AfterState:  "channel:cut",
			IssuedAt:    s.now().UTC(),
		})
		cs.receipts[node] = receiptID
	}
	s.cuts[sentenceID] = cs

	rep := s.summarizeLocked(sentenceID, reason)
	// At broadcast every node is still pending, so every hosting node is
	// conservatively flagged untrusted on the chain until its receipt lands.
	for _, node := range rep.UntrustedNodes {
		s.events.Append(domain.Event{
			Type:       domain.EventNodeMarkedUntrusted,
			AgentID:    agentID,
			SentenceID: sentenceID,
			Actor:      "identity-layer",
			Payload:    map[string]string{"nodeId": node, "reason": "communication cut not acknowledged yet", "simulated": "true"},
		})
	}
	return rep, nil
}

// AckNode records that nodeID acknowledged the channel-cut receipt for
// sentenceID, then re-projects the live group state. An acknowledged node
// leaves the untrusted set; nodes still silent stay untrusted and keep their
// collateral agents frozen.
func (s *Service) AckNode(sentenceID, nodeID, nodeKey string) (*CutReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, ok := s.cuts[sentenceID]
	if !ok {
		return nil, fmt.Errorf("identity: no communication cut recorded for sentence %q", sentenceID)
	}
	receiptID, ok := cs.receipts[nodeID]
	if !ok {
		return nil, fmt.Errorf("identity: node %q holds no receipt for sentence %q", nodeID, sentenceID)
	}
	if _, found := s.receipts.Ack(receiptID, nodeKey, s.now().UTC()); !found {
		return nil, fmt.Errorf("identity: receipt %q not found", receiptID)
	}
	s.events.Append(domain.Event{
		Type:       domain.EventReceiptAcked,
		AgentID:    cs.agentID,
		SentenceID: sentenceID,
		Actor:      "identity-layer",
		Payload:    map[string]string{"nodeId": nodeID, "receiptId": receiptID, "simulated": "true"},
	})
	return s.summarizeLocked(sentenceID, ""), nil
}

// summarizeLocked projects the live cut state. It must be called with s.mu
// held. Untrusted nodes = nodes we broadcast to whose receipt the audit book
// still judges non-acked (the book is the single source of truth).
func (s *Service) summarizeLocked(sentenceID, reason string) *CutReport {
	cs := s.cuts[sentenceID]
	rep := &CutReport{SentenceID: sentenceID, AgentID: cs.agentID}

	broadcast := make([]string, 0, len(cs.receipts))
	for node := range cs.receipts {
		broadcast = append(broadcast, node)
	}
	sort.Strings(broadcast)
	rep.BroadcastNodes = broadcast

	untrustedSet := make(map[string]struct{})
	for _, n := range s.receipts.UntrustedNodes() {
		untrustedSet[n] = struct{}{}
	}
	for _, node := range broadcast {
		if _, bad := untrustedSet[node]; !bad {
			rep.AcknowledgedNodes = append(rep.AcknowledgedNodes, node)
		}
	}
	for _, node := range broadcast {
		if _, bad := untrustedSet[node]; bad {
			rep.UntrustedNodes = append(rep.UntrustedNodes, node)
		}
	}

	// Collateral group freeze: every agent on every untrusted node is frozen,
	// not only the disposed agent.
	for _, node := range rep.UntrustedNodes {
		for _, agent := range s.topology.AgentsOn(node) {
			rep.FrozenAgents = append(rep.FrozenAgents, FreezeOrder{
				AgentID:     agent,
				NodeID:      node,
				SentenceID:  sentenceID,
				Reason:      reason,
				GroupFreeze: agent != cs.agentID,
			})
		}
	}
	return rep
}

// joinSorted renders a string slice as a comma-separated deterministic list
// for event payloads.
func joinSorted(in []string) string {
	if len(in) == 0 {
		return ""
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	b := ""
	for i, s := range out {
		if i > 0 {
			b += ","
		}
		b += s
	}
	return b
}

// --- no-op sinks used when a dependency is intentionally absent -------------

type noopRevocations struct{}

func (noopRevocations) RevokeCredential(string, domain.CredentialKind, domain.AgentID, string, string, []string) {
}

type noopEvents struct{}

func (noopEvents) Append(ev domain.Event) domain.Event { return ev }

type noopReceipts struct{}

func (noopReceipts) RegisterPending(r domain.Receipt) domain.Receipt { return r }
func (noopReceipts) Ack(string, string, time.Time) (domain.Receipt, bool) {
	return domain.Receipt{}, false
}
func (noopReceipts) UntrustedNodes() []string { return nil }
