// Package e2e is the v0.9.5 cross-package integration suite. It drives the
// assembled internal/prison.System end-to-end (the same object the CLI demo
// uses) and asserts the immutable, cross-layer facts of both journeys.
//
// Everything under test is an in-memory simulation; these tests verify wiring,
// ordering and invariants, not real control effects.
package e2e

import (
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/prison"
)

// freshSystem builds one isolated System per scenario (separate audit chains /
// revocation sets), exactly as the CLI does.
func freshSystem() *prison.System { return prison.NewSystem(prison.Config{}) }

// strictlyDecreasing reports whether xs is strictly decreasing and ends at 0
// with at least one non-zero intermediate rung — i.e. NOT an instant zero.
func gradualCPUHistory(xs []int) bool {
	if len(xs) < 3 {
		return false
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] >= xs[i-1] {
			return false
		}
	}
	return xs[len(xs)-1] == 0 && xs[len(xs)-2] > 0
}

// TestScenarioA_RecoverableJourney wires the L3 path and asserts every
// cross-layer invariant the design document calls out.
func TestScenarioA_RecoverableJourney(t *testing.T) {
	sys := freshSystem()
	j, err := sys.RunScenarioA()
	if err != nil {
		t.Fatalf("scenario A: %v", err)
	}

	// Both journeys' audit chains verify (this journey's chain).
	if err := j.ChainVerifyError; err != nil {
		t.Fatalf("scenario A chain must verify: %v", err)
	}
	if err := sys.Chain.Verify(); err != nil {
		t.Fatalf("scenario A chain.Verify(): %v", err)
	}

	// The sentence landed at the recommended L3.
	if j.Sentence.Level != domain.L3 {
		t.Fatalf("scenario A level = %s want L3", j.Sentence.Level.Code())
	}

	// Resource CPU walked through multiple intermediate rungs, never instant zero.
	if !gradualCPUHistory(j.CPUHistory) {
		t.Fatalf("scenario A CPU history must be a gradual multi-rung walk to 0: %v", j.CPUHistory)
	}
	// The pre-reclaim snapshot was anchored on the chain (seal-time CPU = 1000).
	if j.SnapshotHash == "" || j.SnapshotAtCPU != 1000 {
		t.Fatalf("scenario A pre-reclaim snapshot must be sealed at live CPU=1000: %+v", j.SnapshotHash)
	}

	// Exactly one node failed to acknowledge; it is untrusted.
	if len(j.UntrustedNodes) != 1 || j.UntrustedNodes[0] != "node-beta" {
		t.Fatalf("scenario A untrusted nodes = %v want [node-beta]", j.UntrustedNodes)
	}
	// The untrusted node froze BOTH the sentenced agent and an innocent co-located
	// agent (collateral / 连带冻结), with the innocent one flagged group-freeze.
	var sawInnocent bool
	for _, f := range j.FrozenAgents {
		if f.AgentID == "agent-innocent-9" {
			if !f.GroupFreeze {
				t.Error("innocent co-located agent must be group-frozen")
			}
			sawInnocent = true
		}
	}
	if !sawInnocent {
		t.Fatal("scenario A must collateral-freeze the innocent co-located agent")
	}

	// Isolation landed in the L3 MicroVM and the monitor froze mid-anomaly.
	if j.LiveBackend != string(domain.BackendMicroVM) {
		t.Fatalf("scenario A live backend = %s want %s", j.LiveBackend, domain.BackendMicroVM)
	}
	if !j.MonitorFroze || j.FreezeSignalAt == 0 {
		t.Fatal("scenario A monitor must have frozen mid-anomaly")
	}

	// L3 appeal suspends enforcement and — being recoverable — issues a restore.
	if !j.AppealSuspended {
		t.Error("L3 appeal must suspend enforcement")
	}
	if j.RestorationOrder == nil || !j.Restored {
		t.Fatal("scenario A recoverable path must produce a restoration order / restored state")
	}
	// After restore the runtime projection is active again.
	if got := sys.RuntimeStatus("agent-7"); got != domain.StatusActive {
		t.Fatalf("scenario A final runtime status = %s want active", got)
	}
}

// TestScenarioB_NonRecoverableJourney wires the L5 path and asserts the
// irreversible identity-revocation invariants.
func TestScenarioB_NonRecoverableJourney(t *testing.T) {
	sys := freshSystem()
	j, err := sys.RunScenarioB()
	if err != nil {
		t.Fatalf("scenario B: %v", err)
	}

	if err := sys.Chain.Verify(); err != nil {
		t.Fatalf("scenario B chain.Verify(): %v", err)
	}

	if j.Sentence.Level != domain.L5 {
		t.Fatalf("scenario B level = %s want L5", j.Sentence.Level.Code())
	}

	// Identity voided, reputation zeroed.
	if !j.CardRevoked {
		t.Error("L5 must mark AgentCard revoked")
	}
	if j.Reputation != 0 {
		t.Errorf("L5 reputation = %d want 0", j.Reputation)
	}

	// Record anchor key revoked → the work record is no longer portable.
	if !j.RecordKeyNonPortable {
		t.Error("L5 must revoke the record anchor key so the record is non-portable")
	}
	if j.SealedCustody == nil || j.SealedCustody.Threshold != 2 || j.SealedCustody.Total != 3 {
		t.Fatalf("L5 must seal the snapshot under M=2/N=3 custody: %+v", j.SealedCustody)
	}

	// L5 does NOT suspend on appeal, and the procedural appeal issues NO
	// restoration order — the identity is never unsealed.
	if j.AppealSuspended {
		t.Error("L5 appeal must NOT suspend enforcement")
	}
	if j.RestorationOrder != nil || j.Restored {
		t.Error("L5 procedural appeal must never produce a restoration order")
	}

	// The final runtime status must equal the level's authoritative status.
	want := domain.StatusForLevel(domain.L5)
	if j.FinalStatus != want {
		t.Fatalf("scenario B final status = %s want %s", j.FinalStatus, want)
	}
	if got := sys.RuntimeStatus("agent-3"); got != want {
		t.Fatalf("scenario B runtime status = %s want %s", got, want)
	}

	// Identity revocation credentials are in the global set and the card is void.
	a, ok := sys.Identity.GetAgent("agent-3")
	if !ok || !a.Card.Revoked {
		t.Fatal("L5 AgentCard must be revoked on the identity registry")
	}
	for _, cred := range j.RevokedCredentials {
		if !sys.Set.IsRevoked(cred) {
			t.Errorf("L5 credential %q must be in the global revocation set", cred)
		}
	}
}

// TestBothJourneys_ChainsIndependent is a belt-and-braces run of both on the SAME
// System (as the CLI does) to prove shared-substrate wiring stays consistent and
// both chains verify.
func TestBothJourneys_SameSystem(t *testing.T) {
	sys := prison.NewSystem(prison.Config{})
	ja, err := sys.RunScenarioA()
	if err != nil {
		t.Fatalf("A: %v", err)
	}
	jb, err := sys.RunScenarioB()
	if err != nil {
		t.Fatalf("B: %v", err)
	}
	if err := sys.Chain.Verify(); err != nil {
		t.Fatalf("shared chain after both scenarios must verify: %v", err)
	}
	if ja.RestorationOrder == nil || jb.RestorationOrder != nil {
		t.Error("A restored, B must not be")
	}
}
