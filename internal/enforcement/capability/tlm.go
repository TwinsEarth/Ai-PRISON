package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// TlmTier is the capability tier selected by the (simulated) compute-graph key.
type TlmTier string

const (
	TierPrivate TlmTier = "private" // full private capability layer
	TierPublic  TlmTier = "public"  // downgraded public config
)

type tlmState struct {
	tier         TlmTier
	privateLayer bool // private capability layer present
	graphHash    string
	hashEpoch    int
}

// TLModel implements path 3 (TLM tiered model): the SAME weights support
// different capability tiers chosen by a compute-graph key. Downgrading to the
// public tier drops the private capability layer with no retraining / full
// deletion, and mints a new weight/compute-graph hash that goes onto the audit
// chain.
//
// Simulated: there are no real weights, no real compute graph and no real
// tiering — the "hash" is a SHA-256 over the simulated tier/epoch.
type TLModel struct {
	mu     sync.Mutex
	models map[domain.AgentID]*tlmState
}

// NewTLModel returns an empty tiered-model registry.
func NewTLModel() *TLModel {
	return &TLModel{models: make(map[domain.AgentID]*tlmState)}
}

// Provision places the agent on the full private tier. Idempotent.
func (m *TLModel) Provision(agentID domain.AgentID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.models[agentID]; ok {
		return
	}
	st := &tlmState{tier: TierPrivate, privateLayer: true}
	st.graphHash = m.hash(agentID, st)
	m.models[agentID] = st
}

// Downgrade moves the agent to the public tier: the private capability layer
// is lost with no retraining / full deletion, and a NEW compute-graph hash is
// minted. Downgrading an already-public model is idempotent (the hash epoch
// does not change again). Returns the new hash.
func (m *TLModel) Downgrade(agentID domain.AgentID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.models[agentID]
	if !ok {
		return "", fmt.Errorf("capability: agent %s not provisioned in TLM", agentID)
	}
	if st.tier == TierPublic {
		return st.graphHash, nil
	}
	st.tier = TierPublic
	st.privateLayer = false
	st.hashEpoch++
	st.graphHash = m.hash(agentID, st)
	return st.graphHash, nil
}

// PrivateLayerAvailable reports whether the private capability layer is on.
func (m *TLModel) PrivateLayerAvailable(agentID domain.AgentID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.models[agentID]
	return ok && st.privateLayer
}

// Hash returns the current weight/compute-graph hash.
func (m *TLModel) Hash(agentID domain.AgentID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.models[agentID]; ok {
		return st.graphHash
	}
	return ""
}

// Tier returns the current tier ("" for an unknown agent).
func (m *TLModel) Tier(agentID domain.AgentID) TlmTier {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.models[agentID]; ok {
		return st.tier
	}
	return ""
}

// hash computes the deterministic simulated weight/compute-graph digest.
func (m *TLModel) hash(agentID domain.AgentID, st *tlmState) string {
	payload := fmt.Sprintf("tlm|%s|%s|privateLayer=%t|epoch=%d|simulated",
		agentID, st.tier, st.privateLayer, st.hashEpoch)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// summary renders tier state as a deterministic string for the fingerprint.
func (m *TLModel) summary(agentID domain.AgentID) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.models[agentID]
	if !ok {
		return "tlm=none;"
	}
	return "tlm=" + string(st.tier) + ";epoch=" + strconv.Itoa(st.hashEpoch) + ";"
}
