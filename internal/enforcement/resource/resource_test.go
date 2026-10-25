package resource

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/revocation"
)

const testAgent domain.AgentID = "agent-7"

func baseQuota() Quota {
	return Quota{
		AgentID:      testAgent,
		CredentialID: "cred-resource-1",
		CPUMillis:    1000,
		MemoryBytes:  4 << 30,
		StorageBytes: 10 << 30,
		NetworkOpen:  true,
	}
}

// ① A revoked credential must be rejected by Allocate even though it is still
// "valid" (not expired) — the cardinal cache-window bug.
func TestAllocateRejectedOnceCredentialRevoked(t *testing.T) {
	set := revocation.NewSet()
	be := NewInMemoryBackend(set)

	if err := be.Allocate(baseQuota()); err != nil {
		t.Fatalf("pre-revocation allocation should succeed: %v", err)
	}

	// Write the global revocation set exactly as Deprive step 1 does.
	set.Revoke(revocation.Entry{
		ID:      "cred-resource-1",
		AgentID: testAgent,
		Kind:    domain.CredentialResource,
	}, nil)

	// A fresh allocation attempt with the same credential must be refused.
	if err := be.Allocate(baseQuota()); err == nil {
		t.Error("allocation with a revoked credential must be rejected")
	} else {
		t.Logf("rejected as expected: %v", err)
	}

	// Stop-allocation gate must also refuse new work.
	be.StopAllocation(testAgent)
	if !be.AllocationStopped(testAgent) {
		t.Error("AllocationStopped should report true after StopAllocation")
	}
}

// ② Gradual downgrade: multiple strictly-decreasing intermediate rungs,
// ending exactly at zero, never a single jump from 1000 to 0.
func TestGradualReclaimNeverInstantZero(t *testing.T) {
	be := NewInMemoryBackend(nil)
	if err := be.Allocate(baseQuota()); err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if err := be.PlanReclaim(testAgent, DefaultSteps); err != nil {
		t.Fatalf("plan: %v", err)
	}

	var history []int
	for rung := 1; rung <= DefaultSteps; rung++ {
		q, err := be.ReclaimStep(testAgent)
		if err != nil {
			t.Fatalf("rung %d: %v", rung, err)
		}
		history = append(history, q.CPUMillis)
	}

	want := []int{750, 500, 250, 0}
	if fmt.Sprint(history) != fmt.Sprint(want) {
		t.Fatalf("cpu history = %v want %v", history, want)
	}
	// Strictly decreasing, and at least one intermediate state between 1000 and 0.
	if history[0] >= 1000 {
		t.Error("first rung must be below the starting quota")
	}
	if history[len(history)-2] <= 0 {
		t.Error("penultimate rung must still carry quota: instant zero is forbidden")
	}
	for i := 1; i < len(history); i++ {
		if history[i] >= history[i-1] {
			t.Fatalf("rungs must strictly decrease: %v", history)
		}
	}
	if history[len(history)-1] != 0 {
		t.Error("final rung must land exactly on zero")
	}

	if err := be.FreezeMemoryPages(testAgent); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if err := be.CutNetwork(testAgent); err != nil {
		t.Fatalf("cut network: %v", err)
	}
	q, _ := be.Current(testAgent)
	if !q.Zeroed() {
		t.Errorf("quota after full reclaim = %+v want zeroed", q)
	}
}

// buildEngine wires the real revocation set, chain, receipt book and snapshot
// store so orchestration order is observable end-to-end.
func buildEngine() (*Engine, *revocation.Set, *audit.Chain, *audit.ReceiptBook, *MemSnapshotStore) {
	set := revocation.NewSet()
	chain := audit.NewChain()
	book := audit.NewReceiptBook()
	snaps := NewMemSnapshotStore()
	be := NewInMemoryBackend(set)
	engine := NewEngine(Config{
		Backend:     be,
		Revocations: NewRevocationSetSink(set),
		Events:      NewChainSink(chain),
		Receipts:    NewReceiptBookSink(book),
		Snapshots:   snaps,
	})
	return engine, set, chain, book, snaps
}

func depriveInput() DepriveInput {
	return DepriveInput{
		SentenceID:   "sent-1",
		AgentID:      testAgent,
		CredentialID: "cred-resource-1",
		Reason:       "resource abuse",
		HoldingNodes: []string{"node-a", "node-b"},
		Steps:        DefaultSteps,
	}
}

// ③ The snapshot must be sealed on the live pre-reclaim environment and its
// hash must land on the chain BEFORE the first reclaim event.
func TestSnapshotSealedBeforeReclaimAndHashedOnChain(t *testing.T) {
	engine, set, chain, _, snaps := buildEngine()
	be := engine.Backend.(*InMemoryBackend)
	if err := be.Allocate(baseQuota()); err != nil {
		t.Fatalf("allocate: %v", err)
	}

	rep, err := engine.Deprive(depriveInput())
	if err != nil {
		t.Fatalf("deprive: %v", err)
	}

	// Snapshot taken pre-reclaim: CPU recorded at seal time must be 1000.
	if rep.Snapshot.Quota.CPUMillis != 1000 {
		t.Errorf("snapshot quota cpu = %d want 1000 (must be pre-reclaim)", rep.Snapshot.Quota.CPUMillis)
	}
	if rep.Snapshot.ContentHash == "" {
		t.Error("snapshot must carry a content hash")
	}
	if _, ok := snaps.Get(rep.Snapshot.ID); !ok {
		t.Error("snapshot must be persisted in the store")
	}

	// Chain order: revocation -> allocation stopped -> snapshot sealed ->
	// resource reclaimed (repeated). Snapshot must precede the first reclaim.
	entries := chain.Entries()
	var snapIdx, firstReclaimIdx, revokeIdx, stopIdx int
	firstReclaimIdx = -1
	for i, ev := range entries {
		switch ev.Type {
		case domain.EventRevocationWritten:
			revokeIdx = i
		case domain.EventAllocationStopped:
			stopIdx = i
		case domain.EventSnapshotSealed:
			snapIdx = i
		case domain.EventResourceReclaimed:
			if firstReclaimIdx < 0 {
				firstReclaimIdx = i
			}
		}
	}
	if !(revokeIdx < stopIdx && stopIdx < snapIdx && snapIdx < firstReclaimIdx) {
		t.Fatalf("order violated: revoke=%d stop=%d snapshot=%d firstReclaim=%d", revokeIdx, stopIdx, snapIdx, firstReclaimIdx)
	}

	// The snapshot event must carry the anchor hash, and the chain must verify.
	for _, ev := range entries {
		if ev.Type == domain.EventSnapshotSealed && ev.Payload["contentHash"] != rep.Snapshot.ContentHash {
			t.Error("snapshot event must carry the sealed content hash")
		}
	}
	if err := chain.Verify(); err != nil {
		t.Fatalf("chain after deprivation must verify: %v", err)
	}
	if !set.IsRevoked("cred-resource-1") {
		t.Error("credential must be present in the global revocation set")
	}
}

// ④ Non-acknowledged holding nodes must be exposed by the orchestration and
// judged untrusted by the audit receipt book.
func TestUnacknowledgedNodesExposed(t *testing.T) {
	engine, _, _, book, _ := buildEngine()
	be := engine.Backend.(*InMemoryBackend)
	if err := be.Allocate(baseQuota()); err != nil {
		t.Fatalf("allocate: %v", err)
	}

	rep, err := engine.Deprive(depriveInput())
	if err != nil {
		t.Fatalf("deprive: %v", err)
	}

	if len(rep.PendingAcks) != 2 {
		t.Fatalf("pending acknowledgements = %d want 2", len(rep.PendingAcks))
	}
	// No node has acked yet: both are untrusted.
	if got := book.UntrustedNodes(); fmt.Sprint(got) != "[node-a node-b]" {
		t.Fatalf("untrusted before acks = %v want [node-a node-b]", got)
	}

	// node-a confirms; node-b stays silent.
	if _, ok := book.Ack("sent-1|snapshot.seal|node-a", "key-node-a", time.Now()); !ok {
		t.Fatal("ack for node-a receipt failed")
	}
	if got := book.UntrustedNodes(); fmt.Sprint(got) != "[node-b]" {
		t.Fatalf("untrusted after one ack = %v want [node-b]", got)
	}
}

// ⑤ Re-running Deprive must not duplicate the revocation entry, must not mint
// a second snapshot, and must not re-walk the CPU rungs.
func TestDepriveIdempotent(t *testing.T) {
	engine, set, _, _, snaps := buildEngine()
	be := engine.Backend.(*InMemoryBackend)
	if err := be.Allocate(baseQuota()); err != nil {
		t.Fatalf("allocate: %v", err)
	}

	if _, err := engine.Deprive(depriveInput()); err != nil {
		t.Fatalf("first deprive: %v", err)
	}
	historyFirst, _ := be.Current(testAgent)

	if _, err := engine.Deprive(depriveInput()); err != nil {
		t.Fatalf("second deprive: %v", err)
	}

	if set.Len() != 1 {
		t.Errorf("revocation set entries = %d want 1 (idempotent)", set.Len())
	}
	if snaps.Len() != 1 {
		t.Errorf("sealed snapshots = %d want 1 (idempotent)", snaps.Len())
	}
	after, _ := be.Current(testAgent)
	if after.CPUMillis != historyFirst.CPUMillis {
		t.Error("second run must not change the already-zeroed quota")
	}
}

// ⑥ Concurrency: concurrent deprivations and allocation attempts must race
// clean under -race and never corrupt bookkeeping.
func TestConcurrentDepriveAndAllocate(t *testing.T) {
	engine, _, _, _, _ := buildEngine()
	be := engine.Backend.(*InMemoryBackend)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			agent := domain.AgentID(fmt.Sprintf("agent-%d", n))
			q := baseQuota()
			q.AgentID = agent
			q.CredentialID = fmt.Sprintf("cred-%d", n)
			_ = be.Allocate(q)
			in := depriveInput()
			in.AgentID = agent
			in.CredentialID = q.CredentialID
			in.SentenceID = fmt.Sprintf("sent-%d", n)
			_, _ = engine.Deprive(in)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 8; i++ {
		q, ok := be.Current(domain.AgentID(fmt.Sprintf("agent-%d", i)))
		if !ok {
			t.Errorf("agent-%d missing", i)
			continue
		}
		if !q.Zeroed() {
			t.Errorf("agent-%d quota = %+v want zeroed", i, q)
		}
	}
}

// TestDepriveNilSnapshotStoreReturnsError guards against a nil SnapshotStore:
// previously Deprive would nil-panic at the Seal call; now it returns a clear
// configuration error.
func TestDepriveNilSnapshotStoreReturnsError(t *testing.T) {
	set := revocation.NewSet()
	eng := NewEngine(Config{
		Backend: NewInMemoryBackend(set),
		// Snapshots intentionally nil.
	})
	_, err := eng.Deprive(DepriveInput{
		SentenceID:   "s-1",
		AgentID:      testAgent,
		CredentialID: "cred-resource-1",
		Reason:       "regression: nil snapshot store",
	})
	if err == nil {
		t.Fatal("expected error when SnapshotStore is nil, got nil")
	}
	// Also: a nil Backend must error before any state mutation.
	eng2 := NewEngine(Config{Snapshots: &MemSnapshotStore{}})
	if _, err := eng2.Deprive(DepriveInput{AgentID: testAgent}); err == nil {
		t.Fatal("expected error when Backend is nil, got nil")
	}
}
