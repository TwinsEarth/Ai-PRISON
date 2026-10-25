package migration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/revocation"
)

const testAgent domain.AgentID = "agent-mig-1"

func usableCredential(id string) domain.Credential {
	now := time.Now().UTC()
	return domain.Credential{
		ID:       id,
		AgentID:  testAgent,
		Kind:     domain.CredentialCapability,
		State:    domain.CredentialActive,
		IssuedAt: now.Add(-time.Hour),
	}
}

// ① A hardware-bound credential verifies on the bound machine and fails on a
// different machine (a migrated copy). Revocation still wins first.
func TestHardwareBindingMatchVsMismatch(t *testing.T) {
	binder := NewMemHardwareBinder()
	set := revocation.NewSet()
	cred := usableCredential("cred-cap-1")

	if err := binder.Bind(cred.ID, testAgent, "fp-host-A"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	now := time.Now().UTC()

	// Same fingerprint → usable.
	if ok, reason := binder.Validate(cred, now, "fp-host-A", set); !ok {
		t.Errorf("credential should verify on the bound host: %s", reason)
	}
	// Migrated copy on another host → fingerprint mismatch.
	if ok, reason := binder.Validate(cred, now, "fp-host-B", set); ok {
		t.Error("credential must fail on an unbound host")
	} else {
		t.Logf("rejected on migrated host: %s", reason)
	}

	// Revocation beats the fingerprint: even on the bound host a revoked
	// credential is rejected.
	set.Revoke(revocation.Entry{ID: cred.ID, AgentID: testAgent, Kind: domain.CredentialCapability}, nil)
	if ok, _ := binder.Validate(cred, now, "fp-host-A", set); ok {
		t.Error("a revoked credential must be rejected even on the bound host")
	}

	// An unbound credential verifies anywhere (binding is opt-in per cred).
	other := usableCredential("cred-cap-unbound")
	if ok, _ := binder.Validate(other, now, "fp-anywhere", set); !ok {
		t.Error("an unbound credential should verify on any machine")
	}
}

// ② After the portable-record anchor key is revoked, the record (with its
// reputation/history) is no longer portable — a migrated copy carries no
// inherited identity.
func TestRecordKeyRevokedBlocksMigration(t *testing.T) {
	k := NewMemRecordKeeper()
	key := k.Anchor(testAgent)
	rec := WorkRecord{AgentID: testAgent, AnchorKey: key, Reputation: 500, Lessons: []string{"l1"}}

	if !k.Portable(rec) {
		t.Fatal("a record under a live anchor key should be portable")
	}
	// L5: revoke the anchor key.
	k.RevokeAnchor(key)
	if k.Portable(rec) {
		t.Error("after anchor-key revocation the record must NOT be portable")
	}
	if k.Active(key) {
		t.Error("revoked anchor key must report inactive")
	}
	// Revoking twice is idempotent.
	k.RevokeAnchor(key)
}

// ③ Multi-party custody: below the M-of-N threshold unseal fails and leaves an
// audit trace; at/above the threshold it succeeds and also leaves a trace.
func TestSnapshotCustodyThreshold(t *testing.T) {
	chain := audit.NewChain()
	store := NewMemCustodyStore(NewChainSink(chain))

	custodians := []string{"court", "escrow", "trustee"}
	const M = 2
	sealed, err := store.Seal("snap-1", testAgent, "hash-abc", M, custodians)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed.Threshold != M || sealed.Total != 3 {
		t.Fatalf("unexpected threshold/total: %+v", sealed)
	}

	shareOf := func(cust string) Share {
		tok, ok := store.ShareToken("snap-1", cust)
		if !ok {
			t.Fatalf("no share for custodian %s", cust)
		}
		return Share{Custodian: cust, Token: tok}
	}

	before := chain.Len()

	// Only 1 of M=2 shares → denied.
	if _, err := store.Unseal("snap-1", []Share{shareOf("court")}); err == nil {
		t.Error("unseal below the threshold must fail")
	} else {
		t.Logf("denied below threshold: %v", err)
	}

	// Wrong-token share does not count.
	bad := []Share{{Custodian: "court", Token: "wrong"}, shareOf("escrow")}
	if _, err := store.Unseal("snap-1", bad); err != nil {
		t.Logf("mixed bad+good still denied because bad share dropped and good< M: %v", err)
	}

	// 2 valid distinct shares → unsealed.
	content, err := store.Unseal("snap-1", []Share{shareOf("court"), shareOf("escrow")})
	if err != nil {
		t.Fatalf("unseal at threshold must succeed: %v", err)
	}
	if content != "hash-abc" {
		t.Fatalf("unexpected content hash %q", content)
	}

	// Every attempt (seal + 3 unseal attempts) left a chained record.
	if chain.Len() <= before {
		t.Fatal("unseal attempts must be recorded on the audit chain")
	}
	gotAttempt := 0
	for _, ev := range chain.Entries() {
		if ev.Type == domain.EventSnapshotSealed {
			gotAttempt++
		}
	}
	if gotAttempt < 3 {
		t.Errorf("expected seal + at least 2 unseal records, got %d", gotAttempt)
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
}

// ④ Level-driven orchestration: L3 applies hardware binding only; L5 applies
// all three measures and revokes the record anchor key.
func TestSuppressMigrationLevelDiff(t *testing.T) {
	chain := audit.NewChain()
	svc := NewService(Config{Events: NewChainSink(chain)})

	// L3: only hardware binding.
	l3rep, err := svc.SuppressMigration(SuppressInput{
		SentenceID:        "sen-l3",
		AgentID:           testAgent,
		Level:             domain.L3,
		CapabilityCredIDs: []string{"cred-a", "cred-b"},
		Fingerprint:       "fp-l3",
	})
	if err != nil {
		t.Fatalf("L3 suppress: %v", err)
	}
	if len(l3rep.Applied) != 1 || l3rep.Applied[0] != MeasureHardwareBinding {
		t.Errorf("L3 must apply hardware binding only, got %v", l3rep.Applied)
	}
	if len(l3rep.BoundCredentials) != 2 {
		t.Errorf("L3 should bind 2 credentials, got %d", len(l3rep.BoundCredentials))
	}
	if l3rep.Custody.ID != "" {
		t.Error("L3 must NOT seal snapshot custody")
	}

	// L5: all three measures.
	svc5 := NewService(Config{Events: NewChainSink(chain)})
	recKey := svc5.Records.Anchor(testAgent)
	l5rep, err := svc5.SuppressMigration(SuppressInput{
		SentenceID:          "sen-l5",
		AgentID:             testAgent,
		Level:               domain.L5,
		CapabilityCredIDs:   []string{"cred-a"},
		Fingerprint:         "fp-l5",
		SnapshotContentHash: "hash-l5",
		Custodians:          []string{"court", "escrow", "trustee"},
		UnsealThreshold:     2,
		RecordAnchorKey:     recKey,
	})
	if err != nil {
		t.Fatalf("L5 suppress: %v", err)
	}
	got := map[string]bool{}
	for _, m := range l5rep.Applied {
		got[m] = true
	}
	for _, want := range []string{MeasureHardwareBinding, MeasureRecordKeyRevoke, MeasureSnapshotCustody} {
		if !got[want] {
			t.Errorf("L5 must apply %s, applied=%v", want, l5rep.Applied)
		}
	}
	if l5rep.Custody.Threshold != 2 || l5rep.Custody.Total != 3 {
		t.Errorf("L5 custody not sealed with M-of-N: %+v", l5rep.Custody)
	}
	if svc5.Records.Active(recKey) {
		t.Error("L5 must have revoked the record anchor key")
	}

	// The orchestrator emitted one migration-suppressed event per run.
	suppressed := 0
	for _, ev := range chain.Entries() {
		if ev.Type == domain.EventMigrationSuppressed {
			suppressed++
		}
	}
	if suppressed != 2 {
		t.Errorf("expected 2 migration.suppressed events, got %d", suppressed)
	}
}

// ⑤ Idempotent and race-clean: repeated orchestration does not duplicate state,
// and concurrent calls are safe under -race.
func TestSuppressMigrationIdempotentAndConcurrent(t *testing.T) {
	chain := audit.NewChain()
	svc := NewService(Config{Events: NewChainSink(chain)})

	in := SuppressInput{
		SentenceID:          "sen-race",
		AgentID:             testAgent,
		Level:               domain.L5,
		CapabilityCredIDs:   []string{"cred-x"},
		Fingerprint:         "fp-race",
		SnapshotContentHash: "hash-race",
		Custodians:          []string{"court", "escrow"},
		UnsealThreshold:     1,
		RecordAnchorKey:     svc.Records.Anchor(testAgent),
	}

	// Sequential idempotency.
	first, err := svc.SuppressMigration(in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.SuppressMigration(in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(first.Applied) != len(second.Applied) {
		t.Errorf("idempotency: applied set changed between runs (%v vs %v)", first.Applied, second.Applied)
	}
	if binder, ok := svc.Binder.(*MemHardwareBinder); !ok || binder.Count() != 1 {
		t.Errorf("binding must not be duplicated on re-run")
	}
	if cust, ok := svc.Custody.(*MemCustodyStore); !ok || cust.Len() != 1 {
		t.Errorf("snapshot custody must not be re-sealed on re-run")
	}

	// Concurrency under -race.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = svc.SuppressMigration(in)
			_ = svc.Records.Portable(WorkRecord{AgentID: testAgent, AnchorKey: in.RecordAnchorKey})
			cred := usableCredential(fmt.Sprintf("cred-race-%d", i))
			_ = svc.Binder.Bind(cred.ID, testAgent, "fp-race")
			_, _ = svc.Binder.Validate(cred, time.Now().UTC(), "fp-race", nil)
		}(i)
	}
	wg.Wait()
}
