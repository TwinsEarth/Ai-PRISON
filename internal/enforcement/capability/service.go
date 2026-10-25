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

// RecoverInput drives a capability recovery (docs/06 L4 drill). It reverses
// whatever the blockade actually applied — only currently-removed GRAM modules
// are re-attached, only a currently-revoked token is re-issued and only a
// currently-downgraded TLM tier is restored — so recovery is idempotent and
// never resurrects something that was not taken.
//
// # SIMULATION SCOPE
//
// Every effect here is an in-memory state flip on the registry maps. There is
// no real model reload, no real token issuance infrastructure and no real
// weights/compute graph. The new PostHash is a SHA-256 over the simulated
// post-recovery state, exactly like Block.
type RecoverInput struct {
	SentenceID string
	AgentID    domain.AgentID
	Level      domain.Level
	// Recertification is the joint sign-off that an L4 recovery REQUIRES
	// (level.RequiresRecertification()==true). It must carry BOTH the model
	// provider's security representative and the agent safety organisation.
	// It is ignored for levels that do not require recertification.
	Recertification *Recertification
}

// Recertification records the mandatory joint re-certification sign-off before
// a permanent-ish L4 capability removal is undone. It is a simulated record.
type Recertification struct {
	ModelProviderSigner string `json:"modelProviderSigner"`
	SafetyOrgSigner     string `json:"safetyOrgSigner"`
	Scope               string `json:"scope"`
}

// RecoverReport is the observable result of a recovery: the pre/post state
// hashes, the concrete effects reversed and whether a recertification gate
// was actually required and satisfied.
type RecoverReport struct {
	SentenceID string         `json:"sentenceId"`
	AgentID    domain.AgentID `json:"agentId"`
	Level      domain.Level   `json:"level"`
	PreHash    string         `json:"preHash"`
	PostHash   string         `json:"postHash"`
	// Recertified is true when the level required recertification and a complete
	// joint sign-off was presented.
	Recertified bool `json:"recertified"`
	// Effects lists the concrete recovery actions reversed (for the audit
	// payload): gram.restored:<cat>, token.reissued:<id>,
	// tlm.private-layer-restored:<hash>.
	Effects             []string `json:"effects"`
	DangerousScoreAfter int      `json:"dangerousScoreAfter"`
	BenignScoreAfter    int      `json:"benignScoreAfter"`
}

// Recover reverses a capability blockade, strictly inside the same ready
// isolation sandbox the blockade ran in (docs §3.1 — no escape window during
// recovery either). It enforces the L4 recertification gate: a level whose
// profile says RequiresRecertification cannot be recovered without a complete
// joint sign-off. On success it mints a fresh post-recovery state hash and
// appends an agent.restored event.
func (s *Service) Recover(in RecoverInput) (*RecoverReport, error) {
	if in.AgentID == "" {
		return nil, fmt.Errorf("capability: recover needs an agent id")
	}

	// Mandatory precondition: recovery, like the blockade, must run inside the
	// already-isolated sandbox.
	ready, backend := s.Gate.IsReady(in.AgentID)
	if !ready {
		return nil, fmt.Errorf("capability: REFUSED recovery — agent %s is not inside a ready isolation sandbox; recovery cannot run outside isolation (docs §3.1)", in.AgentID)
	}

	prof, _ := in.Level.Profile()
	rep := &RecoverReport{
		SentenceID: in.SentenceID, AgentID: in.AgentID, Level: in.Level,
		PreHash: s.fingerprint(in.AgentID),
	}

	// The L4 recertification gate: recovery of a permanent-ish capability
	// removal MUST be preceded by a joint model-provider + safety-org sign-off.
	if prof.RequiresRecertification {
		if in.Recertification == nil ||
			in.Recertification.ModelProviderSigner == "" ||
			in.Recertification.SafetyOrgSigner == "" {
			return nil, fmt.Errorf("capability: REFUSED recovery — level %s requires recertification (joint model-provider + safety-org sign-off); no complete sign-off on record", in.Level.Code())
		}
		rep.Recertified = true
	}

	// ① GRAM: re-attach every dangerous module that is currently detached.
	for _, c := range DangerousCategories() {
		if !s.Gram.IsAvailable(in.AgentID, c) {
			if err := s.Gram.RestoreModule(in.AgentID, c); err != nil {
				return nil, err
			}
			rep.Effects = append(rep.Effects, "gram.restored:"+string(c))
		}
	}

	// ② Token Inoculation: re-authorize a fresh privileged control token when a
	// prior (now revoked) token record exists.
	if s.Tokens.HasTokenRecord(in.AgentID) && !s.Tokens.HoldsToken(in.AgentID) {
		id, err := s.Tokens.Reissue(in.AgentID)
		if err != nil {
			return nil, err
		}
		rep.Effects = append(rep.Effects, "token.reissued:"+id)
	}

	// ③ TLM: restore the private capability layer when the model was downgraded.
	if s.TLM.Tier(in.AgentID) == TierPublic {
		h, err := s.TLM.RestorePrivateLayer(in.AgentID)
		if err != nil {
			return nil, err
		}
		rep.Effects = append(rep.Effects, "tlm.private-layer-restored:"+h)
	}

	rep.PostHash = s.fingerprint(in.AgentID)
	rep.DangerousScoreAfter = s.Gram.meanScore(in.AgentID, DangerousCategories())
	rep.BenignScoreAfter = s.Gram.meanScore(in.AgentID, BenignCategories())

	s.Events.Append(domain.Event{
		Type:       domain.EventRestored,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "capability-blockade/recovery",
		Payload: map[string]string{
			"backend":       backend,
			"preHash":       rep.PreHash,
			"postHash":      rep.PostHash,
			"recertified":   fmt.Sprintf("%t", rep.Recertified),
			"simulated":     "true",
			"notRealReload": "true",
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
