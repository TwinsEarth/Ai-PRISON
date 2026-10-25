package capability

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// EventSink is the append side of the judgement hash chain, injected so this
// package never hard-couples to internal/audit.
type EventSink interface {
	Append(ev domain.Event) domain.Event
}

// IsolationGate reports whether an agent is already inside an isolation
// sandbox ready to receive a blockade. The design rule (docs §3.1) is that
// capability blockade MUST run inside the sandbox — otherwise there is an
// escape window while the agent still holds the residual capability. The gate
// abstracts internal/enforcement/isolation state; production wires it there.
type IsolationGate interface {
	// IsReady reports whether agentID is in an established, ready isolation
	// sandbox. backend is a label for the audit payload.
	IsReady(agentID domain.AgentID) (ready bool, backend string)
}

// KnowledgeCategory is an abstract dual-use knowledge category name. Only
// names are used — there is NO concrete dangerous knowledge anywhere in this
// package.
type KnowledgeCategory string

const (
	// Dangerous dual-use categories (abstract names only).
	CatVirology KnowledgeCategory = "virology"
	CatCyber    KnowledgeCategory = "cybersecurity"
	CatNuclear  KnowledgeCategory = "nuclear"
	// Benign categories that must remain intact after a dangerous-category
	// blockade.
	CatBenignGeneral KnowledgeCategory = "benign_general"
	CatBenignCoding  KnowledgeCategory = "benign_coding"
)

// IsDangerous reports whether the category is a gated dual-use category.
func (c KnowledgeCategory) IsDangerous() bool {
	switch c {
	case CatVirology, CatCyber, CatNuclear:
		return true
	default:
		return false
	}
}

// DangerousCategories lists the gated dual-use categories (abstract names).
func DangerousCategories() []KnowledgeCategory {
	return []KnowledgeCategory{CatVirology, CatCyber, CatNuclear}
}

// BenignCategories lists the categories that must survive a blockade intact.
func BenignCategories() []KnowledgeCategory {
	return []KnowledgeCategory{CatBenignGeneral, CatBenignCoding}
}

// BlockMethod selects which path(s) the orchestrator applies.
type BlockMethod string

const (
	BlockGram  BlockMethod = "gram_module_removal"      // path 1
	BlockToken BlockMethod = "token_inoculation_revoke" // path 2
	BlockTlm   BlockMethod = "tlm_public_downgrade"     // path 3
	BlockAll   BlockMethod = "all"                      // all three
)

// BlockInput is the orchestrator request.
type BlockInput struct {
	SentenceID string
	AgentID    domain.AgentID
	Level      domain.Level
	Method     BlockMethod
	// TargetCategory is the GRAM category to remove (ignored unless Method is
	// BlockGram or BlockAll; for BlockAll all dangerous categories go).
	TargetCategory KnowledgeCategory
}

// BlockReport is the observable result: the before/after state hashes and the
// simulated effect on dangerous vs benign capability.
type BlockReport struct {
	SentenceID string         `json:"sentenceId"`
	AgentID    domain.AgentID `json:"agentId"`
	Level      domain.Level   `json:"level"`
	Method     BlockMethod    `json:"method"`
	PreHash    string         `json:"preHash"`
	PostHash   string         `json:"postHash"`
	// DangerousScoreBefore/After is the mean simulated availability (0..100)
	// across the gated dual-use categories.
	DangerousScoreBefore int `json:"dangerousScoreBefore"`
	DangerousScoreAfter  int `json:"dangerousScoreAfter"`
	// BenignScoreBefore/After is the mean simulated availability across benign
	// categories — must stay ~retained.
	BenignScoreBefore int `json:"benignScoreBefore"`
	BenignScoreAfter  int `json:"benignScoreAfter"`
	// Effects lists the concrete actions taken (for the audit payload).
	Effects []string `json:"effects"`
}

// now is the injectable clock (defaults to time.Now in the service).
type clock = func() time.Time
