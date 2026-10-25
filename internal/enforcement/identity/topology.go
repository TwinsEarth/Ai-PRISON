package identity

import (
	"sort"
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// InMemoryTopology is the simulated node ↔ agent placement table. It lives
// entirely in this process: there are no real nodes, no real sockets and no
// real placement protocol.
type InMemoryTopology struct {
	mu         sync.RWMutex
	nodeAgents map[string]map[domain.AgentID]struct{} // node -> agents placed there
	agentNodes map[domain.AgentID]map[string]struct{} // agent -> nodes carrying its channel
}

// NewInMemoryTopology returns an empty, ready-to-use simulated topology.
func NewInMemoryTopology() *InMemoryTopology {
	return &InMemoryTopology{
		nodeAgents: make(map[string]map[domain.AgentID]struct{}),
		agentNodes: make(map[domain.AgentID]map[string]struct{}),
	}
}

// Register places agentID on nodeID. Re-registering the same pair is a no-op.
func (t *InMemoryTopology) Register(nodeID string, agentID domain.AgentID) {
	if nodeID == "" || agentID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.nodeAgents[nodeID]; !ok {
		t.nodeAgents[nodeID] = make(map[domain.AgentID]struct{})
	}
	t.nodeAgents[nodeID][agentID] = struct{}{}
	if _, ok := t.agentNodes[agentID]; !ok {
		t.agentNodes[agentID] = make(map[string]struct{})
	}
	t.agentNodes[agentID][nodeID] = struct{}{}
}

// Deregister removes every placement of agentID.
func (t *InMemoryTopology) Deregister(agentID domain.AgentID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for node := range t.agentNodes[agentID] {
		delete(t.nodeAgents[node], agentID)
		if len(t.nodeAgents[node]) == 0 {
			delete(t.nodeAgents, node)
		}
	}
	delete(t.agentNodes, agentID)
}

// NodesHosting returns the sorted set of nodes carrying agentID's channel.
func (t *InMemoryTopology) NodesHosting(agentID domain.AgentID) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.agentNodes[agentID]))
	for n := range t.agentNodes[agentID] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// AgentsOn returns the sorted set of agents placed on nodeID.
func (t *InMemoryTopology) AgentsOn(nodeID string) []domain.AgentID {
	t.mu.RLock()
	defer t.mu.RUnlock()
	set := t.nodeAgents[nodeID]
	out := make([]domain.AgentID, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
