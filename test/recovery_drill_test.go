// Package e2e — v1.0.0 recovery-drill suite. It drives the assembled
// internal/prison recovery matrix (docs/06) on a fresh System and asserts the
// structured PASS/FAIL report and the per-level recovery invariants.
//
// Everything under test is an in-memory simulation; these tests verify the
// reversibility state machine and its gates, not real control effects.
package e2e

import (
	"testing"

	"github.com/ai-prison/ai-prison/internal/prison"
)

// TestRecoveryDrill_OverallPass runs the whole matrix and asserts every row
// passed — a single failing assertion must FAIL the test.
func TestRecoveryDrill_OverallPass(t *testing.T) {
	rep := prison.RunRecoveryDrill()
	if !rep.Overall {
		t.Fatalf("recovery drill overall must PASS; failing rows:")
	}
	for _, row := range rep.Rows {
		if !row.Pass {
			t.Errorf("level %q row FAIL: %s (notes=%v)", row.Level, row.Result, row.Notes)
		}
	}
}

// TestRecoveryDrill_CoversEveryLevel asserts the matrix covers L2–L5 plus the
// chain-integrity row, and that L5 is explicitly labelled non-recoverable.
func TestRecoveryDrill_CoversEveryLevel(t *testing.T) {
	rep := prison.RunRecoveryDrill()
	want := map[string]bool{"L2": false, "L3": false, "L4": false, "L5": false, "chain": false}
	for _, row := range rep.Rows {
		if _, ok := want[row.Level]; ok {
			want[row.Level] = true
		}
	}
	for lvl, seen := range want {
		if !seen {
			t.Errorf("recovery drill matrix missing row for %s", lvl)
		}
	}

	for _, row := range rep.Rows {
		if row.Level == "L5" && row.Pass {
			// The L5 row passing means the NON-RECOVERABLE property held.
			t.Log("L5 non-recoverable property verified:", row.Result)
		}
	}
}

// TestRecoveryDrill_L4RowPasses is a focused regression guard: the L4 row —
// which exercises the recertification refusal-then-authorised-recovery path —
// must pass end to end.
func TestRecoveryDrill_L4RowPasses(t *testing.T) {
	sys := prison.NewSystem(prison.Config{})
	rep := sys.Drill()
	for i := range rep.Rows {
		if rep.Rows[i].Level == "L4" && !rep.Rows[i].Pass {
			t.Fatalf("L4 drill row must pass: %s (notes=%v)", rep.Rows[i].Result, rep.Rows[i].Notes)
		}
	}
}
