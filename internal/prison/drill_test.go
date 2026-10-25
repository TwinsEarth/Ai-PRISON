package prison

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// TestRunRecoveryDrillAllPass drives the full recovery matrix in-process and
// asserts the structural invariants the drill is designed to prove. It is the
// in-process counterpart of the cross-process `aiprison drill` e2e test: every
// level row must PASS, the shared hash chain must verify, and the L5 row must
// prove the "non-recoverable" property (no restoration order, identity revoked,
// reputation zero).
func TestRunRecoveryDrillAllPass(t *testing.T) {
	rep := RunRecoveryDrill()
	if rep == nil {
		t.Fatal("RunRecoveryDrill returned nil")
	}
	if rep.RunAt.IsZero() {
		t.Error("drill RunAt is zero")
	}

	seen := map[string]bool{}
	for _, row := range rep.Rows {
		seen[row.Level] = true
		if !row.Pass {
			t.Errorf("level %s FAIL: result=%q notes=%v", row.Level, row.Result, row.Notes)
		}
		if row.Disposition == "" {
			t.Errorf("level %s has no disposition recorded", row.Level)
		}
	}

	for _, want := range []string{domain.L2.Code(), domain.L3.Code(), domain.L4.Code(), domain.L5.Code(), "chain"} {
		if !seen[want] {
			t.Errorf("drill report missing row for %q (have %v)", want, seen)
		}
	}

	if !rep.Overall {
		t.Errorf("expected Overall=true when every row passes; rows=%v", rep.Rows)
	}
}

// TestDrillReportFailurePropagation verifies that a single ok(false) flips the
// row to FAIL and, by the Drill() reduction, flips Overall to false.
func TestDrillReportFailurePropagation(t *testing.T) {
	rep := &DrillReport{}
	row := rep.Add(DrillRow{Level: "L2"})
	row.ok(true, "positive assertion holds")
	row.ok(false, "negative assertion fails the row")

	if row.Pass {
		t.Error("row must be FAIL after ok(false)")
	}
	if len(row.Notes) != 2 {
		t.Errorf("expected both facts recorded as notes, got %v", row.Notes)
	}

	// Mirror Drill()'s reduction.
	overall := true
	for i := range rep.Rows {
		if !rep.Rows[i].Pass {
			overall = false
		}
	}
	if overall {
		t.Error("Overall must fold a failing row into false")
	}
}

// TestDrillReportPrintSmoke renders the report and checks the summary line and
// that every level appears. It guards against a nil-pointer / index panic in
// the fixed-width table path.
func TestDrillReportPrintSmoke(t *testing.T) {
	rep := RunRecoveryDrill()
	out := rep.Print()
	if !strings.Contains(out, "OVERALL: PASS") {
		t.Errorf("Print output missing PASS summary:\n%s", out)
	}
	for _, want := range []string{"L2", "L3", "L4", "L5"} {
		if !strings.Contains(out, want) {
			t.Errorf("Print output missing level %s:\n%s", want, out)
		}
	}
	// A failing report must render the FAIL summary branch too.
	failRep := &DrillReport{Overall: false}
	failRep.Add(DrillRow{Level: "L5", Disposition: "d", Precondition: "p", Result: "r"})
	if got := failRep.Print(); !strings.Contains(got, "OVERALL: FAIL") {
		t.Errorf("failing report did not render FAIL summary:\n%s", got)
	}
}

// TestTrunc verifies the rune-aware truncation used by the report table.
func TestTrunc(t *testing.T) {
	if got := trunc("hello", 10); got != "hello" {
		t.Errorf("short string must pass through unchanged, got %q", got)
	}
	long := strings.Repeat("中", 30)
	got := trunc(long, 10)
	runes := []rune(got)
	if len(runes) != 10 || runes[len(runes)-1] != '…' {
		t.Errorf("trunc(30 runes,10) must be 10 runes ending with ellipsis, got %q (%d runes)", got, len(runes))
	}
	if got := trunc("", 5); got != "" {
		t.Errorf("empty string must stay empty, got %q", got)
	}
}

// TestOrderIDNil guards the nil-restoration-order accessor used in drillL2/L3.
func TestOrderIDNil(t *testing.T) {
	if orderID(nil) != "" {
		t.Error("orderID(nil) must return empty string")
	}
}

// TestSegmentCountEmptyAndMissing verifies the CLI segment-count helper against
// empty / missing directories and a populated temp dir.
func TestSegmentCountEmptyAndMissing(t *testing.T) {
	if got := SegmentCount(""); got != 0 {
		t.Errorf("SegmentCount(\"\") = %d, want 0", got)
	}
	if got := SegmentCount("/nonexistent/ai-prison-dir-12345"); got != 0 {
		t.Errorf("SegmentCount(missing dir) = %d, want 0", got)
	}
	// A populated temp dir with one synthetic segment file must report 1.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seg-0000000000.log"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := SegmentCount(dir); got != 1 {
		t.Errorf("SegmentCount(populated) = %d, want 1", got)
	}
}
