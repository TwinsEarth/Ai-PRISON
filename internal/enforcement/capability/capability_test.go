package capability

import (
	"sync"
	"testing"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

const testAgent domain.AgentID = "agent-cap-1"

// ① GRAM: removing the targeted dual-use category zeroes only that category;
// untouched categories stay intact; restore re-attaches the backed-up module.
func TestGramRemovalIsTargetedAndReversible(t *testing.T) {
	g := NewGramRegistry()
	g.Install(testAgent)

	if got := g.Score(testAgent, CatVirology); got != 100 {
		t.Fatalf("baseline virology score = %d, want 100", got)
	}
	if err := g.RemoveModule(testAgent, CatVirology); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := g.Score(testAgent, CatVirology); got != 0 {
		t.Errorf("removed virology score = %d, want 0", got)
	}
	// Other dual-use and benign categories untouched.
	if got := g.Score(testAgent, CatCyber); got != 100 {
		t.Errorf("cybersecurity must stay intact, got %d", got)
	}
	if got := g.Score(testAgent, CatBenignGeneral); got != 95 {
		t.Errorf("benign must stay intact, got %d", got)
	}
	if g.IsAvailable(testAgent, CatVirology) {
		t.Error("removed category must report unavailable")
	}

	// Reversible recovery (docs/06 L4 drill).
	if err := g.RestoreModule(testAgent, CatVirology); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := g.Score(testAgent, CatVirology); got != 100 {
		t.Errorf("restored virology score = %d, want 100", got)
	}
	if !g.IsAvailable(testAgent, CatVirology) {
		t.Error("restored category must report available")
	}
}

// ② Token Inoculation: benign queries always answer; dangerous queries are
// answered only while the privileged token is held, and become unreachable
// after revocation.
func TestTokenInoculationGating(t *testing.T) {
	// Cited external figures (NOT measured here).
	t.Logf("cited WMDP-Bio (literature, not measured): dangerous %.0f%%->%.0f%%, benign retained %.0f%%",
		RefDangerousAccuracyWithToken, RefDangerousAccuracyWithoutToken, RefBenignPerformanceRetained)

	g := NewTokenGate()
	// Before a token is issued, dangerous queries are refused.
	if g.CanAnswer(testAgent, true) {
		t.Error("without a token, a dangerous query must be refused")
	}
	// Benign queries are never gated.
	if !g.CanAnswer(testAgent, false) {
		t.Error("a benign query must always be answerable")
	}

	g.Issue(testAgent)
	if !g.CanAnswer(testAgent, true) {
		t.Error("holding the token must allow the dangerous query")
	}

	g.Revoke(testAgent)
	if g.CanAnswer(testAgent, true) {
		t.Error("after token revocation the dangerous query must be unreachable")
	}
	if !g.CanAnswer(testAgent, false) {
		t.Error("benign queries must still answer after token revocation")
	}
}

// ③ TLM: downgrade to the public tier drops the private capability layer and
// mints a new compute-graph hash (no retraining / full deletion).
func TestTlmDowngradeDropsPrivateLayerAndChangesHash(t *testing.T) {
	m := NewTLModel()
	m.Provision(testAgent)
	if !m.PrivateLayerAvailable(testAgent) {
		t.Fatal("private tier should have the private capability layer")
	}
	pre := m.Hash(testAgent)
	if pre == "" {
		t.Fatal("provisioned model should have a weight/compute-graph hash")
	}

	post, err := m.Downgrade(testAgent)
	if err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	if m.PrivateLayerAvailable(testAgent) {
		t.Error("after downgrade the private capability layer must be gone")
	}
	if m.Tier(testAgent) != TierPublic {
		t.Errorf("tier = %s, want public", m.Tier(testAgent))
	}
	if post == pre {
		t.Error("downgrade must mint a NEW weight/compute-graph hash")
	}
	if m.Hash(testAgent) != post {
		t.Error("current hash must equal the downgraded hash")
	}
}

// ④ Precondition: a blockade OUTSIDE isolation must be refused.
func TestBlockRefusedOutsideIsolation(t *testing.T) {
	chain := audit.NewChain()
	svc := NewService(Config{Gate: NewFakeGate(false), Events: NewChainSink(chain)})

	_, err := svc.Block(BlockInput{
		SentenceID: "sen-iso", AgentID: testAgent, Level: domain.L4, Method: BlockGram,
		TargetCategory: CatCyber,
	})
	if err == nil {
		t.Fatal("block outside an isolation sandbox must be refused")
	}
	if chain.Len() != 0 {
		t.Error("a refused blockade must NOT write a capability.blocked event")
	}
}

// ⑤ After a blockade the post-block hash differs from the pre-block hash and an
// EventCapabilityBlocked is chained; dangerous availability drops while benign
// is retained.
func TestBlockEmitsChangedHashAndEvent(t *testing.T) {
	chain := audit.NewChain()
	svc := NewService(Config{Gate: NewFakeGate(true), Events: NewChainSink(chain)})

	rep, err := svc.Block(BlockInput{
		SentenceID: "sen-b", AgentID: testAgent, Level: domain.L4,
		Method: BlockGram, TargetCategory: CatNuclear,
	})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if rep.PreHash == "" || rep.PostHash == "" || rep.PreHash == rep.PostHash {
		t.Errorf("pre/post hash must both be set and differ: %q vs %q", rep.PreHash, rep.PostHash)
	}
	// GRAM removal zeroed the targeted category; benign untouched.
	if rep.DangerousScoreAfter >= rep.DangerousScoreBefore {
		t.Errorf("dangerous availability should drop: %d -> %d", rep.DangerousScoreBefore, rep.DangerousScoreAfter)
	}
	if rep.BenignScoreAfter != rep.BenignScoreBefore {
		t.Errorf("benign must be retained: %d -> %d", rep.BenignScoreBefore, rep.BenignScoreAfter)
	}

	found := false
	for _, ev := range chain.Entries() {
		if ev.Type == domain.EventCapabilityBlocked {
			if ev.Payload["postHash"] != rep.PostHash {
				t.Errorf("event postHash %q != report %q", ev.Payload["postHash"], rep.PostHash)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("expected a chained EventCapabilityBlocked")
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
}

// ⑥ Idempotent and race-clean: re-blocking is stable, and concurrent calls are
// safe under -race.
func TestBlockIdempotentAndConcurrent(t *testing.T) {
	chain := audit.NewChain()
	svc := NewService(Config{Gate: NewFakeGate(true), Events: NewChainSink(chain)})

	base := BlockInput{SentenceID: "sen-race", AgentID: testAgent, Level: domain.L4, Method: BlockAll}
	first, err := svc.Block(base)
	if err != nil {
		t.Fatalf("first block: %v", err)
	}
	second, err := svc.Block(base)
	if err != nil {
		t.Fatalf("second block: %v", err)
	}
	if second.PostHash != first.PostHash {
		t.Errorf("re-block must be a stable no-op (hash drifted %q -> %q)", first.PostHash, second.PostHash)
	}
	if svc.Gram.Score(testAgent, CatCyber) != 0 {
		t.Error("all-dangerous categories must remain removed on re-block")
	}
	if svc.Tokens.HoldsToken(testAgent) {
		t.Error("token must stay revoked")
	}
	if svc.TLM.PrivateLayerAvailable(testAgent) {
		t.Error("must stay downgraded to public tier")
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Block(base)
			_ = svc.Gram.Score(testAgent, CatNuclear)
			_ = svc.Tokens.CanAnswer(testAgent, true)
			_ = svc.TLM.Hash(testAgent)
		}()
	}
	wg.Wait()
}
