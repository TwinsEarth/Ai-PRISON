package revocation

import (
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func sampleEntry() Entry {
	return Entry{
		ID:         "cred-001",
		AgentID:    "agent-alpha",
		Kind:       domain.CredentialResource,
		SentenceID: "sent-001",
		Reason:     "resource abuse",
		RevokedAt:  time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	}
}

// The cardinal gate: a credential that is still inside its validity window
// but present in the global revocation set MUST be rejected, via the real
// Set as the domain.RevocationChecker.
func TestRevokedButNotExpiredRejectedByIsUsable(t *testing.T) {
	set := NewSet()
	set.Revoke(sampleEntry(), nil)

	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	notExpired := now.Add(24 * time.Hour)

	c := domain.Credential{
		ID: "cred-001", State: domain.CredentialActive,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired,
	}
	if c.IsUsable(now, set) {
		t.Error("revoked-but-not-expired credential MUST be unusable (revocation checked before expiry)")
	}

	// A different credential, still in the window, is untouched.
	other := domain.Credential{
		ID: "cred-999", State: domain.CredentialActive,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired,
	}
	if !other.IsUsable(now, set) {
		t.Error("unrevoked in-window credential should be usable")
	}

	// The set itself implements the interface by membership only.
	if !set.IsRevoked("cred-001") {
		t.Error("IsRevoked must report membership")
	}
	if set.IsRevoked("cred-999") {
		t.Error("IsRevoked must not invent membership")
	}
}

// A revoked entry can never be removed: Revoke is idempotent and no delete
// API exists, so repeated writes leave exactly one row.
func TestRevokeIsIdempotentAndRemovalImpossible(t *testing.T) {
	set := NewSet()
	first := set.Revoke(sampleEntry(), []string{"node-a", "node-b"})
	second := set.Revoke(sampleEntry(), []string{"node-x"}) // ignored on replay

	if set.Len() != 1 {
		t.Fatalf("idempotent Revoke must keep one row, got %d", set.Len())
	}
	if first.Enforced || second.Enforced {
		t.Error("entry with unacknowledged nodes must not be enforced yet")
	}
	if len(set.List()) != 1 {
		t.Errorf("List must expose exactly one entry, got %d", len(set.List()))
	}
	if !set.IsRevoked("cred-001") {
		t.Error("entry must still be revoked after replay")
	}
}

func TestEnforcedOnlyAfterAllNodesAck(t *testing.T) {
	set := NewSet()
	set.Revoke(sampleEntry(), []string{"node-a", "node-b"})

	// One of two confirmed -> not enforced, unacknowledged list names node-b.
	if found, enforced := set.AckNode("cred-001", "node-a"); !found || enforced {
		t.Error("single acknowledgement must not flip enforced")
	}
	wantPending := []string{"node-b"}
	pending, _ := set.UnacknowledgedNodes("cred-001")
	if len(pending) != 1 || pending[0] != wantPending[0] {
		t.Errorf("unacknowledged = %v, want %v", pending, wantPending)
	}

	// Both confirmed -> enforced flips and the pending list empties.
	if _, enforced := set.AckNode("cred-001", "node-b"); !enforced {
		t.Error("all nodes acknowledged -> enforced must be true")
	}
	pending, _ = set.UnacknowledgedNodes("cred-001")
	if len(pending) != 0 {
		t.Errorf("after full ack unacknowledged = %v, want empty", pending)
	}

	entry, _ := set.Get("cred-001")
	if !entry.Enforced {
		t.Error("stored entry must be enforced after full acknowledgement")
	}
	if len(entry.AcknowledgedBy) != 2 {
		t.Errorf("acknowledgedBy = %v, want 2 nodes", entry.AcknowledgedBy)
	}
}

func TestRevokeWithoutHoldingNodesIsImmediatelyEnforced(t *testing.T) {
	set := NewSet()
	set.Revoke(sampleEntry(), nil)
	entry, _ := set.Get("cred-001")
	if !entry.Enforced {
		t.Error("no required nodes means vacuously fully acknowledged -> enforced")
	}
	if pending, _ := set.UnacknowledgedNodes("cred-001"); len(pending) != 0 {
		t.Errorf("no holding nodes -> unacknowledged = %v, want empty", pending)
	}
}

func TestAckUnknownCredential(t *testing.T) {
	set := NewSet()
	if found, _ := set.AckNode("nope", "node-a"); found {
		t.Error("ack for unknown credential must report found=false")
	}
	if _, found := set.UnacknowledgedNodes("nope"); found {
		t.Error("unknown credential must not have an unacknowledged list")
	}
}

// Race gate: concurrent Revoke writers and IsRevoked readers must be safe.
func TestConcurrentRevokeAndIsRevoked(t *testing.T) {
	set := NewSet()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			set.Revoke(Entry{ID: "cred", Kind: domain.CredentialCapability}, []string{"node-a"})
			set.IsRevoked("cred")
			set.IsRevoked("other")
			set.AckNode("cred", "node-a")
			set.List()
		}(i)
	}
	wg.Wait()
	if set.Len() != 1 {
		t.Errorf("concentric idempotent writes must leave one row, got %d", set.Len())
	}
}

// List must expose writes in order and return detached snapshots.
func TestListIsOrderedSnapshot(t *testing.T) {
	set := NewSet()
	set.Revoke(Entry{ID: "c1", Kind: domain.CredentialResource}, nil)
	set.Revoke(Entry{ID: "c2", Kind: domain.CredentialIdentity}, nil)
	got := set.List()
	if len(got) != 2 || got[0].ID != "c1" || got[1].ID != "c2" {
		t.Fatalf("List order = %+v", got)
	}
	got[0].Reason = "mutated"
	if again, _ := set.Get("c1"); again.Reason == "mutated" {
		t.Error("List snapshots must be detached copies")
	}
}
