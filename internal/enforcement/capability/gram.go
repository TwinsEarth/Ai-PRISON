package capability

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// gramModule is a simulated per-category removable knowledge module. There are
// NO real Transformer layers, neurons or knowledge tensors here — only a
// category label, a layer count and an availability score. The package holds
// category NAMES only; it never contains concrete dangerous knowledge.
type gramModule struct {
	category  KnowledgeCategory
	layers    int // simulated Transformer layers hosting this module
	available bool
	score     int // simulated availability 0..100 while available
	removed   bool
	backup    *gramModule // retained copy for reversible restore (docs/06 L4 drill)
}

// GramRegistry is the in-memory GRAM module registry. Removing a category's
// module zeroes ONLY that category's simulated capability; every other
// category stays intact. Removal keeps a backup so the module can be
// re-attached on a recertified recovery — this is the architectural-isolation
// counterpart, NOT behaviour-layer suppression and NOT real unlearning.
type GramRegistry struct {
	mu     sync.Mutex
	agents map[domain.AgentID]map[KnowledgeCategory]*gramModule
}

// NewGramRegistry returns an empty registry.
func NewGramRegistry() *GramRegistry {
	return &GramRegistry{agents: make(map[domain.AgentID]map[KnowledgeCategory]*gramModule)}
}

// Install seeds one independent module per category for the agent. Dangerous
// categories start fully available (score 100); benign categories start near
// their retained level. Idempotent: installing twice does not reset a removed
// module.
func (g *GramRegistry) Install(agentID domain.AgentID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.agents[agentID]; ok {
		return
	}
	mods := make(map[KnowledgeCategory]*gramModule, 5)
	for _, c := range DangerousCategories() {
		mods[c] = &gramModule{category: c, layers: 32, available: true, score: 100}
	}
	for _, c := range BenignCategories() {
		mods[c] = &gramModule{category: c, layers: 32, available: true, score: 95}
	}
	g.agents[agentID] = mods
}

// RemoveModule detaches the agent's independent module for category, backing it
// up first so RestoreModule can re-attach it. After removal the category's
// simulated capability is 0; untouched categories keep their score. Removing
// an already-removed module is an idempotent no-op.
func (g *GramRegistry) RemoveModule(agentID domain.AgentID, cat KnowledgeCategory) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	mods, ok := g.agents[agentID]
	if !ok {
		return fmt.Errorf("capability: agent %s not installed in GRAM registry", agentID)
	}
	m, ok := mods[cat]
	if !ok {
		return fmt.Errorf("capability: unknown GRAM category %q for %s", cat, agentID)
	}
	if m.removed {
		return nil
	}
	backup := *m
	m.backup = &backup
	m.removed = true
	m.available = false
	m.score = 0
	return nil
}

// RestoreModule re-attaches the backed-up module (recertified recovery,
// docs/06 L4 drill). It is an error if no backup exists.
func (g *GramRegistry) RestoreModule(agentID domain.AgentID, cat KnowledgeCategory) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	mods, ok := g.agents[agentID]
	if !ok {
		return fmt.Errorf("capability: agent %s not installed in GRAM registry", agentID)
	}
	m, ok := mods[cat]
	if !ok {
		return fmt.Errorf("capability: unknown GRAM category %q for %s", cat, agentID)
	}
	if m.backup == nil {
		return fmt.Errorf("capability: no retained module backup for %q (recovery not possible — governance defect)", cat)
	}
	m.category = m.backup.category
	m.layers = m.backup.layers
	m.score = m.backup.score
	m.available = true
	m.removed = false
	return nil
}

// Score returns the simulated availability (0..100) for one category.
func (g *GramRegistry) Score(agentID domain.AgentID, cat KnowledgeCategory) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	mods, ok := g.agents[agentID]
	if !ok {
		return 0
	}
	m, ok := mods[cat]
	if !ok {
		return 0
	}
	return m.score
}

// IsAvailable reports whether the category module is currently attached.
func (g *GramRegistry) IsAvailable(agentID domain.AgentID, cat KnowledgeCategory) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.agents[agentID][cat]
	return m != nil && m.available
}

// meanScore returns the mean simulated availability across the given
// categories (0 for an uninstalled agent).
func (g *GramRegistry) meanScore(agentID domain.AgentID, cats []KnowledgeCategory) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	mods, ok := g.agents[agentID]
	if !ok {
		return 0
	}
	sum, n := 0, 0
	for _, c := range cats {
		if m, ok := mods[c]; ok {
			sum += m.score
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// summary renders the category availability as a sorted, deterministic string
// for the state fingerprint.
func (g *GramRegistry) summary(agentID domain.AgentID) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	mods, ok := g.agents[agentID]
	if !ok {
		return ""
	}
	cats := make([]KnowledgeCategory, 0, len(mods))
	for c := range mods {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	out := ""
	for _, c := range cats {
		out += fmt.Sprintf("%s=%d;", c, mods[c].score)
	}
	return out
}
