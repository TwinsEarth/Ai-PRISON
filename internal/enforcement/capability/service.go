package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Service orchestrates the three capability-blockade paths behind the mandatory
// isolation-sandbox precondition and records the state transition on the
// judgement chain. Dependencies are small interfaces / in-memory so tests can
// use fakes.
type Service struct {
	Gate   IsolationGate
	Gram   *GramRegistry
	Tokens *TokenGate
	TLM    *TLModel
	Events EventSink
	Now    clock
}

// Config builds a Service; nil fields default to fresh in-memory subsystems.
type Config struct {
	Gate   IsolationGate
	Events EventSink
}

// NewService returns a wired orchestrator.
func NewService(cfg Config) *Service {
	s := &Service{
		Gate:   cfg.Gate,
		Gram:   NewGramRegistry(),
		Tokens: NewTokenGate(),
		TLM:    NewTLModel(),
		Events: cfg.Events,
		Now:    time.Now,
	}
	if s.Events == nil {
		s.Events = noopEvents{}
	}
	return s
}

// Block applies the requested blockade method. It REFUSES unless the agent is
// already inside a ready isolation sandbox (docs §3.1 — no escape window during
// blockade). On success it mints a new post-block state hash (which MUST differ
// from the pre-block hash) and writes EventCapabilityBlocked.
func (s *Service) Block(in BlockInput) (*BlockReport, error) {
	if in.AgentID == "" {
		return nil, fmt.Errorf("capability: block needs an agent id")
	}
	if in.Method == "" {
		in.Method = BlockAll
	}

	// Mandatory precondition: already isolated.
	ready, backend := s.Gate.IsReady(in.AgentID)
	if !ready {
		return nil, fmt.Errorf("capability: REFUSED — agent %s is not inside a ready isolation sandbox; blockade cannot run outside isolation (docs §3.1)", in.AgentID)
	}

	// Ensure the agent has a baseline state to block from.
	s.Gram.Install(in.AgentID)
	s.TLM.Provision(in.AgentID)
	if !s.Tokens.HasTokenRecord(in.AgentID) {
		// baseline = "holds a live token"; an existing (even revoked) record
		// is left untouched so re-blocking is idempotent.
		s.Tokens.Issue(in.AgentID)
	}

	pre := s.fingerprint(in.AgentID)
	beforeDangerous := s.Gram.meanScore(in.AgentID, DangerousCategories())
	beforeBenign := s.Gram.meanScore(in.AgentID, BenignCategories())

	rep := &BlockReport{
		SentenceID: in.SentenceID, AgentID: in.AgentID, Level: in.Level,
		Method: in.Method, PreHash: pre,
		DangerousScoreBefore: beforeDangerous, BenignScoreBefore: beforeBenign,
	}

	switch in.Method {
	case BlockGram, BlockAll:
		cats := DangerousCategories()
		if in.Method == BlockGram {
			cats = []KnowledgeCategory{in.TargetCategory}
		}
		for _, c := range cats {
			if err := s.Gram.RemoveModule(in.AgentID, c); err != nil {
				return nil, err
			}
			rep.Effects = append(rep.Effects, "gram.removed:"+string(c))
		}
	case BlockToken:
		// nothing: handled below uniformly
	case BlockTlm:
		// nothing: handled below uniformly
	}

	if in.Method == BlockToken || in.Method == BlockAll {
		s.Tokens.Revoke(in.AgentID)
		rep.Effects = append(rep.Effects, "token.revoked")
	}
	if in.Method == BlockTlm || in.Method == BlockAll {
		hash, err := s.TLM.Downgrade(in.AgentID)
		if err != nil {
			return nil, err
		}
		rep.Effects = append(rep.Effects, "tlm.downgrade:"+hash)
	}

	rep.PostHash = s.fingerprint(in.AgentID)
	rep.DangerousScoreAfter = s.Gram.meanScore(in.AgentID, DangerousCategories())
	rep.BenignScoreAfter = s.Gram.meanScore(in.AgentID, BenignCategories())

	s.Events.Append(domain.Event{
		Type:       domain.EventCapabilityBlocked,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "capability-blockade",
		Payload: map[string]string{
			"method":            string(in.Method),
			"level":             in.Level.Code(),
			"backend":           backend,
			"preHash":           rep.PreHash,
			"postHash":          rep.PostHash,
			"dangerousBefore":   itoa(rep.DangerousScoreBefore),
			"dangerousAfter":    itoa(rep.DangerousScoreAfter),
			"benignBefore":      itoa(rep.BenignScoreBefore),
			"benignAfter":       itoa(rep.BenignScoreAfter),
			"simulated":         "true",
			"notRealUnlearning": "true",
		},
	})
	return rep, nil
}

// fingerprint hashes the combined simulated state (GRAM module availability +
// token state + TLM tier) into one digest.
func (s *Service) fingerprint(agentID domain.AgentID) string {
	payload := s.Gram.summary(agentID) + "|" + s.Tokens.summary(agentID) + "|" + s.TLM.summary(agentID)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// itoa is a tiny local int->string to avoid importing strconv in the hot path.
func itoa(i int) string { return fmt.Sprintf("%d", i) }

type noopEvents struct{}

func (noopEvents) Append(ev domain.Event) domain.Event { return ev }
