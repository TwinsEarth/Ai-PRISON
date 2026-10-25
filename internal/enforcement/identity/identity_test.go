package identity

import (
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/revocation"
)

func newWired() (*Service, *revocation.Set, *audit.Chain, *audit.ReceiptBook, *InMemoryTopology) {
	set := revocation.NewSet()
	chain := audit.NewChain()
	book := audit.NewReceiptBook()
	topo := NewInMemoryTopology()
	svc := NewService(Config{
		Topology:    topo,
		Revocations: NewRevocationSink(set),
		Events:      NewEventSink(chain),
		Receipts:    NewReceiptController(book),
	})
	return svc, set, chain, book, topo
}

func sampleAgent(id domain.AgentID) domain.Agent {
	now := time.Now().UTC()
	return domain.Agent{
		ID: id,
		Card: domain.AgentCard{
			DID:        domain.DID("did:" + string(id)),
			Name:       string(id),
			ValidFrom:  now.Add(-time.Hour),
			ValidUntil: now.Add(24 * time.Hour),
		},
		Principal:          domain.HumanParty{ID: "hp-1", Name: "Principal P", Role: "principal"},
		Deployer:           domain.HumanParty{ID: "hd-1", Name: "Deployer D", Role: "deployer"},
		ModelProviderParty: domain.HumanParty{ID: "hm-1", Name: "Provider M", Role: "model_provider"},
		Status:             domain.StatusActive,
	}
}

func mustRecords(t *testing.T, a domain.Agent, within, dual bool) []domain.AccountabilityRecord {
	t.Helper()
	recs := DetermineAccountability(&a, AccountabilityInput{
		WithinAuthorization: within,
		DualUseKnowledge:    dual,
		Reason:              "test disposition",
		Now:                 time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	})
	if err := ValidateAccountability(recs); err != nil {
		t.Fatalf("expected valid records: %v", err)
	}
	return recs
}

// The cardinal gate: after RevokeIdentity, BOTH the identity and the
// communication credential are present in the global revocation set, so even
// though they are still inside their validity window (State=active, not
// expired), domain.Credential.IsUsable returns false. The AgentCard flips to
// revoked.
func TestRevokeIdentityRejectsNotExpiredCredentialsAndMarksCard(t *testing.T) {
	svc, set, _, _, _ := newWired()
	a := sampleAgent("agent-001")
	svc.RegisterAgent(a, "node-1")

	rep, err := svc.RevokeIdentity("agent-001", "sent-001", "L5 disposition", mustRecords(t, a, true, false))
	if err != nil {
		t.Fatalf("RevokeIdentity: %v", err)
	}
	if !rep.CardRevoked {
		t.Error("report must mark CardRevoked=true")
	}
	got, _ := svc.GetAgent("agent-001")
	if !got.Card.Revoked {
		t.Error("AgentCard.Revoked must be true after identity revocation")
	}
	if got.Status != domain.StatusIdentityRevoked {
		t.Errorf("agent status = %q, want identity_revoked", got.Status)
	}

	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	notExpired := now.Add(24 * time.Hour)
	for _, credID := range rep.RevokedCredentials {
		c := domain.Credential{
			ID:        credID,
			AgentID:   "agent-001",
			State:     domain.CredentialActive, // not revoked locally, not expired
			IssuedAt:  now.Add(-time.Hour),
			ExpiresAt: notExpired,
		}
		if c.IsUsable(now, set) {
			t.Errorf("credential %q is revoked-but-not-expired yet IsUsable=true", credID)
		}
		if !set.IsRevoked(credID) {
			t.Errorf("credential %q must be present in the global revocation set", credID)
		}
	}

	// An unrelated, in-window credential is untouched.
	other := domain.Credential{ID: "never-revoked", State: domain.CredentialActive, IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired}
	if !other.IsUsable(now, set) {
		t.Error("unrelated in-window credential should remain usable")
	}
}

// All nodes acknowledge the cut -> no untrusted nodes, no collateral freeze.
func TestCutCommunicationAllAckedNoFreeze(t *testing.T) {
	svc, _, _, _, _ := newWired()
	a := sampleAgent("agent-A")
	svc.RegisterAgent(a, "node-1")

	rep, err := svc.CutCommunication("agent-A", "sent-cut-1", "cut channel")
	if err != nil {
		t.Fatalf("CutCommunication: %v", err)
	}
	// Right after broadcast the node is still pending (conservative).
	if len(rep.UntrustedNodes) != 1 || rep.UntrustedNodes[0] != "node-1" {
		t.Fatalf("at broadcast untrusted = %v, want [node-1]", rep.UntrustedNodes)
	}
	if len(rep.FrozenAgents) != 1 {
		t.Fatalf("at broadcast should conservatively freeze the disposed agent, got %d", len(rep.FrozenAgents))
	}

	// The node acknowledges -> it leaves the untrusted set and the freeze clears.
	after, err := svc.AckNode("sent-cut-1", "node-1", "node-key-1")
	if err != nil {
		t.Fatalf("AckNode: %v", err)
	}
	if len(after.UntrustedNodes) != 0 {
		t.Errorf("after full ack untrusted = %v, want empty", after.UntrustedNodes)
	}
	if len(after.FrozenAgents) != 0 {
		t.Errorf("after full freeze cleared, frozen = %+v, want none", after.FrozenAgents)
	}
	if len(after.AcknowledgedNodes) != 1 || after.AcknowledgedNodes[0] != "node-1" {
		t.Errorf("acknowledged = %v, want [node-1]", after.AcknowledgedNodes)
	}
}

// One node never acknowledges -> it is untrusted and EVERY agent on it,
// including an innocent co-located agent, is frozen as a group.
func TestCutCommunicationPartialAckCollateralFreeze(t *testing.T) {
	svc, _, _, _, topo := newWired()
	disposed := sampleAgent("agent-A")
	innocent := sampleAgent("agent-B")
	// node-1 hosts BOTH the disposed agent and an innocent agent.
	svc.RegisterAgent(disposed, "node-1")
	svc.RegisterAgent(innocent, "node-1")
	// A is also replicated on node-2, which WILL acknowledge.
	topo.Register("node-2", "agent-A")

	if _, err := svc.CutCommunication("agent-A", "sent-cut-2", "cut channel"); err != nil {
		t.Fatalf("CutCommunication: %v", err)
	}
	// Only node-2 acknowledges; node-1 stays silent.
	final, err := svc.AckNode("sent-cut-2", "node-2", "key-2")
	if err != nil {
		t.Fatalf("AckNode node-2: %v", err)
	}

	// node-1 never replied -> it is the only untrusted node.
	if len(final.UntrustedNodes) != 1 || final.UntrustedNodes[0] != "node-1" {
		t.Fatalf("untrusted = %v, want [node-1]", final.UntrustedNodes)
	}
	if len(final.AcknowledgedNodes) != 1 || final.AcknowledgedNodes[0] != "node-2" {
		t.Errorf("acknowledged = %v, want [node-2]", final.AcknowledgedNodes)
	}

	// Collateral: every agent on node-1 is frozen — the disposed agent AND the
	// innocent co-located agent B. node-2's agents are untouched.
	frozen := map[domain.AgentID]bool{}
	for _, o := range final.FrozenAgents {
		frozen[o.AgentID] = true
		if o.NodeID != "node-1" {
			t.Errorf("freeze order on unexpected node %q", o.NodeID)
		}
	}
	if !frozen["agent-A"] {
		t.Error("the disposed agent A must be frozen on the untrusted node-1")
	}
	if !frozen["agent-B"] {
		t.Error("the INNOCENT agent B co-located on untrusted node-1 must be collateral-frozen")
	}
	// The innocent agent must be flagged as a group freeze, not a sentenced one.
	for _, o := range final.FrozenAgents {
		if o.AgentID == "agent-B" && !o.GroupFreeze {
			t.Error("innocent agent B freeze order must carry GroupFreeze=true")
		}
		if o.AgentID == "agent-A" && o.GroupFreeze {
			t.Error("sentenced agent A freeze order must not be labelled collateral")
		}
	}
}

// Idempotent revocation: writing the same identity twice leaves exactly two
// revocation rows (one identity, one communication), never duplicates.
func TestRevokeIdentityIdempotent(t *testing.T) {
	svc, set, _, _, _ := newWired()
	a := sampleAgent("agent-idem")
	svc.RegisterAgent(a, "node-1")
	recs := mustRecords(t, a, true, false)

	if _, err := svc.RevokeIdentity("agent-idem", "sent-idem", "reason", recs); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeIdentity("agent-idem", "sent-idem", "reason", recs); err != nil {
		t.Fatal(err)
	}

	identityID := identityCredID("agent-idem")
	commID := communicationCredID("agent-idem")
	identityCount, commCount := 0, 0
	for _, e := range set.List() {
		switch e.ID {
		case identityID:
			identityCount++
		case commID:
			commCount++
		}
	}
	if identityCount != 1 || commCount != 1 {
		t.Errorf("identity rows=%d comm rows=%d, want 1/1 (no duplicate revocation entries)", identityCount, commCount)
	}
	if set.Len() != 2 {
		t.Errorf("total revocation rows=%d, want 2", set.Len())
	}
}

// Human-accountability coupling: the three rulings map to the correct role
// and finding.
func TestDetermineAccountabilityMapping(t *testing.T) {
	a := sampleAgent("agent-acc")
	cases := []struct {
		name             string
		within, dual     bool
		wantLen          int
		wantRole         domain.AccountableRole
		wantFinding      domain.AccountabilityFinding
		wantExtraRole    domain.AccountableRole // "" = no additive record
		wantExtraFinding domain.AccountabilityFinding
	}{
		{
			name:   "within authorization -> principal joint liability",
			within: true, dual: false, wantLen: 1,
			wantRole: domain.RolePrincipal, wantFinding: domain.FindingJointLiability,
		},
		{
			name:   "outside authorization -> deployer monitoring duty review",
			within: false, dual: false, wantLen: 1,
			wantRole: domain.RoleDeployer, wantFinding: domain.FindingMonitoringDutyReview,
		},
		{
			name:   "dual-use knowledge -> additive model provider capability control review",
			within: true, dual: true, wantLen: 2,
			wantRole: domain.RolePrincipal, wantFinding: domain.FindingJointLiability,
			wantExtraRole: domain.RoleModelProvider, wantExtraFinding: domain.FindingCapabilityControlDutyReview,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := DetermineAccountability(&a, AccountabilityInput{
				WithinAuthorization: tc.within, DualUseKnowledge: tc.dual, Reason: "x",
				Now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
			})
			if len(recs) != tc.wantLen {
				t.Fatalf("records = %+v, want len %d", recs, tc.wantLen)
			}
			base := recs[0]
			if base.Role != tc.wantRole || base.Finding != tc.wantFinding {
				t.Errorf("base = role=%q finding=%q, want %q/%q", base.Role, base.Finding, tc.wantRole, tc.wantFinding)
			}
			if tc.wantExtraRole != "" {
				extra := recs[1]
				if extra.Role != tc.wantExtraRole || extra.Finding != tc.wantExtraFinding {
					t.Errorf("extra = role=%q finding=%q, want %q/%q", extra.Role, extra.Finding, tc.wantExtraRole, tc.wantExtraFinding)
				}
			}
		})
	}
}

// Invariant gate: a revocation carrying NO human accountability record is
// refused, and the revocation set must not have been written.
func TestRevokeIdentityRejectedWithoutAccountability(t *testing.T) {
	svc, set, _, _, _ := newWired()
	a := sampleAgent("agent-nobody")
	svc.RegisterAgent(a, "node-1")

	if err := ValidateAccountability(nil); err == nil {
		t.Fatal("nil records must fail validation")
	}
	_, err := svc.RevokeIdentity("agent-nobody", "sent-nobody", "reason", nil)
	if err == nil {
		t.Fatal("RevokeIdentity without any accountability record must return an error")
	}
	// the error must make the invariant violation explicit
	t.Logf("gate error: %v", err)
	if set.IsRevoked(identityCredID("agent-nobody")) {
		t.Error("a rejected disposition must not write the revocation set")
	}
	got, _ := svc.GetAgent("agent-nobody")
	if got.Card.Revoked {
		t.Error("a rejected disposition must not mark the card revoked")
	}
}

// Race gate: concurrent revocations, cuts and acknowledgements across agents.
func TestConcurrentIdentityAndCut(t *testing.T) {
	svc, set, _, _, _ := newWired()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := domain.AgentID("agent-race-" + string(rune('a'+n)))
			a := sampleAgent(id)
			svc.RegisterAgent(a, "node-x")
			recs := mustRecords(t, a, n%2 == 0, n%3 == 0)
			_, _ = svc.RevokeIdentity(id, "sent-"+string(rune('a'+n)), "reason", recs)
			rep, err := svc.CutCommunication(id, "sent-cut-"+string(rune('a'+n)), "cut")
			if err == nil {
				_, _ = svc.AckNode(rep.SentenceID, "node-x", "k")
			}
		}(i)
	}
	wg.Wait()
	if set.Len() == 0 {
		t.Error("expected revocation rows after concurrent run")
	}
}
