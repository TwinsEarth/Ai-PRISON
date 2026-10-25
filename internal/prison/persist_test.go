package prison

import (
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/store"
)

// These tests prove the v1.1 event-sourcing wiring in-process (no subprocess):
// a System built on a real FileStore runs both scenarios, is closed, and a
// second System re-opens the SAME data dir. The durable record must rebuild the
// exact governance state, and the v1.0 invariants must survive the restart.
//
// Every path uses t.TempDir(); nothing writes into the repo.

func mustScenarioBoth(t *testing.T, sys *System) {
	t.Helper()
	if _, err := sys.RunScenarioA(); err != nil {
		t.Fatalf("scenario A: %v", err)
	}
	if _, err := sys.RunScenarioB(); err != nil {
		t.Fatalf("scenario B: %v", err)
	}
}

// TestRestartRebuildsGovernanceState runs A+B on a FileStore System, closes,
// reopens the same dir and asserts the rebuilt InspectView matches the facts
// the first process knew.
func TestRestartRebuildsGovernanceState(t *testing.T) {
	dir := t.TempDir()

	// --- process 1: write the ledger --------------------------------------
	fs1, err := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	sys1 := NewSystem(Config{Store: fs1})
	if sys1.BootstrapErr != nil {
		t.Fatalf("bootstrap: %v", sys1.BootstrapErr)
	}
	mustScenarioBoth(t, sys1)
	if err := sys1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	recordsAfterRun := fs1.Len()
	if recordsAfterRun == 0 {
		t.Fatal("expected records to be persisted")
	}

	// --- process 2: rebuild from the same data dir -------------------------
	fs2, err := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	sys2 := NewSystem(Config{Store: fs2})
	if sys2.BootstrapErr != nil {
		t.Fatalf("replay bootstrap: %v", sys2.BootstrapErr)
	}
	if !sys2.restored {
		t.Fatal("second System should have rebuilt by replay")
	}
	if got := fs2.Len(); got != recordsAfterRun {
		t.Fatalf("record count after restart = %d want %d", got, recordsAfterRun)
	}

	v := sys2.InspectView()
	if !v.ChainVerifyOK {
		t.Fatalf("chain must verify after replay: %s", v.ChainVerifyNote)
	}
	if v.ChainEvents == 0 {
		t.Fatal("replayed chain must carry events")
	}

	// Per-agent projection: agent-7 L3 restored/active; agent-3 L5 sealed.
	byID := map[string]AgentView{}
	for _, a := range v.Agents {
		byID[a.AgentID] = a
	}
	a7, ok := byID["agent-7"]
	if !ok {
		t.Fatal("agent-7 must be rebuilt from the ledger")
	}
	if a7.Status != string(domain.StatusActive) || a7.Level != "L3" {
		t.Fatalf("agent-7 = %+v want L3/active (restored)", a7)
	}
	a3, ok := byID["agent-3"]
	if !ok {
		t.Fatal("agent-3 must be rebuilt from the ledger")
	}
	if a3.Status != string(domain.StatusIdentityRevoked) || a3.Level != "L5" {
		t.Fatalf("agent-3 = %+v want L5/identity_revoked", a3)
	}
	if !a3.CardRevoked {
		t.Fatal("agent-3 AgentCard must still be revoked after restart")
	}
	if a3.Reputation != 0 {
		t.Fatalf("agent-3 reputation = %d want 0 after restart", a3.Reputation)
	}
	if v.Sentences != 2 || v.Cases != 2 {
		t.Fatalf("sentences/cases = %d/%d want 2/2", v.Sentences, v.Cases)
	}
	if v.RevocationEntries < 1 {
		t.Fatal("expected persisted revocation entries")
	}
	if len(v.Snapshots) != 2 {
		t.Fatalf("snapshots = %d want 2", len(v.Snapshots))
	}

	// The cardinal invariant: the L5 identity credential is revoked-but-not-expired
	// and must stay unusable AFTER RESTART, consulted through the rebuilt set.
	for _, cred := range []string{"identity:agent-3", "communication:agent-3"} {
		if !sys2.Set.IsRevoked(cred) {
			t.Fatalf("revoked credential %q must be present in the rebuilt set", cred)
		}
		c := domain.Credential{ID: cred, State: domain.CredentialActive}
		if c.IsUsable(sys2.Clock(), sys2.Set) {
			t.Fatalf("revoked-but-not-expired credential %q must remain IsUsable()==false after restart", cred)
		}
	}
	// L5 has NO restoration order on record.
	if _, has := sys2.Gov.GetRestorationOrder("sentence-2"); has {
		t.Fatal("L5 must never have a restoration order")
	}
	_ = fs2.Close()
}

// TestRestartThenAppendKeepsChain proves a second process can keep appending to
// the same ledger (append-only, no rewrite): the chain verifies and the new
// run does not duplicate already-recorded entries.
func TestRestartThenAppendKeepsChain(t *testing.T) {
	dir := t.TempDir()

	fs1, _ := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	sys1 := NewSystem(Config{Store: fs1})
	if _, err := sys1.RunScenarioA(); err != nil {
		t.Fatal(err)
	}
	if err := sys1.Close(); err != nil {
		t.Fatal(err)
	}
	afterA := fs1.Len()

	// Reopen and run scenario B on top.
	fs2, _ := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	sys2 := NewSystem(Config{Store: fs2})
	if sys2.BootstrapErr != nil {
		t.Fatal(sys2.BootstrapErr)
	}
	if _, err := sys2.RunScenarioB(); err != nil {
		t.Fatal(err)
	}
	if err := sys2.Close(); err != nil {
		t.Fatal(err)
	}
	if fs2.Len() <= afterA {
		t.Fatalf("append run must grow the ledger: before=%d after=%d", afterA, fs2.Len())
	}

	// A third reopen must still verify (append never rewrote history).
	ro, err := store.OpenReadonly(dir)
	if err != nil {
		t.Fatalf("verify after append: %v", err)
	}
	if err := store.Verify(collectAll(t, ro)); err != nil {
		t.Fatalf("ledger verify: %v", err)
	}
	_ = ro.Close()
}

// collectAll is a tiny replay helper for the read-only store above.
func collectAll(t *testing.T, s *store.FileStore) []store.Record {
	t.Helper()
	var out []store.Record
	if err := s.Replay(nil, func(r store.Record) error { out = append(out, r); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestMemoryVsFileProjectionEqual compares the key inspect facts between a
// pure in-memory System and a file-backed System rebuilt from the ledger —
// the view must be backend-independent.
func TestMemoryVsFileProjectionEqual(t *testing.T) {
	// in-memory
	mem := NewSystem(Config{})
	mustScenarioBoth(t, mem)
	mv := mem.InspectView()

	// file-backed + restart
	dir := t.TempDir()
	fs, _ := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	w := NewSystem(Config{Store: fs})
	mustScenarioBoth(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fs2, _ := store.NewFileStore(dir, store.Options{Clock: DefaultClock})
	r := NewSystem(Config{Store: fs2})
	if r.BootstrapErr != nil {
		t.Fatal(r.BootstrapErr)
	}
	rv := r.InspectView()
	_ = fs2.Close()

	if mv.ChainVerifyOK != rv.ChainVerifyOK || !rv.ChainVerifyOK {
		t.Fatalf("chain verify memory=%v file=%v", mv.ChainVerifyOK, rv.ChainVerifyOK)
	}
	if mv.ChainEvents != rv.ChainEvents {
		t.Fatalf("chain events memory=%d file=%d", mv.ChainEvents, rv.ChainEvents)
	}
	if mv.RevocationEntries != rv.RevocationEntries {
		t.Fatalf("revocation entries memory=%d file=%d", mv.RevocationEntries, rv.RevocationEntries)
	}
	if mv.Sentences != rv.Sentences || mv.Cases != rv.Cases {
		t.Fatalf("sentences/cases memory=%d/%d file=%d/%d",
			mv.Sentences, mv.Cases, rv.Sentences, rv.Cases)
	}
	if len(mv.Snapshots) != len(rv.Snapshots) {
		t.Fatalf("snapshot count memory=%d file=%d", len(mv.Snapshots), len(rv.Snapshots))
	}
	if len(mv.Agents) != len(rv.Agents) {
		t.Fatalf("agent rows memory=%d file=%d", len(mv.Agents), len(rv.Agents))
	}
	for i := range rv.Agents {
		if rv.Agents[i].AgentID == "agent-3" && !rv.Agents[i].CardRevoked {
			t.Fatal("file projection must show agent-3 cardRevoked")
		}
	}
}

// TestNilStoreIsUnchanged proves the default path wires no recorder and the
// ledger remains absent (v1.0 behaviour).
func TestNilStoreIsUnchanged(t *testing.T) {
	sys := NewSystem(Config{})
	if sys.recorder != nil {
		t.Fatal("nil Store must leave the recorder absent")
	}
	if sys.restored {
		t.Fatal("nil Store must not be a restored system")
	}
	mustScenarioBoth(t, sys)
	if err := sys.Close(); err != nil {
		t.Fatal(err)
	}
	v := sys.InspectView()
	if v.StoreRecords != 0 || v.DataDir != "" {
		t.Fatalf("memory mode must report no ledger: %+v", v)
	}
}
