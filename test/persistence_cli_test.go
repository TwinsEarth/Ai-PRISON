// Package e2e holds the v1.1 cross-process persistence assertions. Unlike the
// in-package prison tests, these drive a REAL compiled aiprison binary built
// into a t.TempDir() and assert across process boundaries:
//
//   - demo writes a ledger in one process; inspect/verify read it in another;
//   - drill writes a ledger; inspect/verify re-read it;
//   - fault injection: byte-tamper a published record → verify must fail and
//     locate the tamper; truncate the tail → repair-tail recovers and verify
//     returns to green.
//
// Everything exercised is the governance/audit record; the execution backends
// remain simulated. Strictly defensive — traceability and integrity only.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// e2eBin is the compiled CLI built once by TestMain into a temp dir.
var e2eBin string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		// No toolchain: the build steps are next stage's concern.
		println("go toolchain not found; skipping cross-process persistence e2e")
		os.Exit(0)
	}
	work, err := os.MkdirTemp("", "aiprison-e2e")
	if err != nil {
		println("mkdir:", err.Error())
		os.Exit(1)
	}
	e2eBin = filepath.Join(work, "aiprison")
	build := exec.Command("go", "build", "-o", e2eBin, "./cmd/aiprison")
	build.Dir = moduleRoot()
	if out, err := build.CombinedOutput(); err != nil {
		println("build aiprison failed:", string(out))
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(work)
	os.Exit(code)
}

// moduleRoot is the repo root (this package lives in test/).
func moduleRoot() string {
	wd, _ := os.Getwd()
	return filepath.Dir(wd)
}

// runCLI executes the built binary and returns combined output + exit code.
func runCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(e2eBin, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return string(out), code
}

// extractCount pulls the "已落盘记录数" number the demo/drill prints.
var recordCountRe = regexp.MustCompile(`已落盘记录数:\s*(\d+)`)

// TestDemo_Inspect_Verify_CrossProcess writes a ledger with demo in one process
// and reads it with inspect/verify in another.
func TestDemo_Inspect_Verify_CrossProcess(t *testing.T) {
	dir := t.TempDir()

	demoOut, code := runCLI(t, "demo", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("demo exit=%d out=\n%s", code, demoOut)
	}
	m := recordCountRe.FindStringSubmatch(demoOut)
	if m == nil {
		t.Fatalf("demo output missing 已落盘记录数 line:\n%s", demoOut)
	}
	demoCount := m[1]

	// Another process: read-only inspect.
	inspOut, code := runCLI(t, "inspect", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("inspect exit=%d out=\n%s", code, inspOut)
	}
	for _, want := range []string{
		"CHAIN VERIFY:    OK",
		"agent-7", "level=L3", "status=active",
		"agent-3", "level=L5", "status=identity_revoked", "cardRevoked=true",
	} {
		if !strings.Contains(inspOut, want) {
			t.Errorf("inspect output missing %q:\n%s", want, inspOut)
		}
	}
	if strings.Contains(inspOut, "agent-3") {
		if !regexp.MustCompile(`agent-3\s+level=L5\s+status=identity_revoked\s+cardRevoked=true`).MatchString(inspOut) {
			t.Errorf("agent-3 projection malformed:\n%s", inspOut)
		}
	}
	// Record count shown by inspect equals the demo's last summary line.
	if !strings.Contains(inspOut, "记录数/段数:      "+demoCount+" /") {
		t.Errorf("inspect record count %q mismatch demo %q:\n%s", demoCount, demoCount, inspOut)
	}
	// Revocation entries and snapshot metadata are present.
	if !strings.Contains(inspOut, "吊销条目:        ") {
		t.Errorf("inspect missing revocation count:\n%s", inspOut)
	}
	if !strings.Contains(inspOut, "snap-agent-7") || !strings.Contains(inspOut, "snap-agent-3") {
		t.Errorf("inspect missing snapshot metadata:\n%s", inspOut)
	}

	// verify exits 0 on the healthy ledger.
	if vOut, vc := runCLI(t, "verify", "--data-dir", dir); vc != 0 {
		t.Fatalf("verify exit=%d out=%s", vc, vOut)
	}
}

// TestDrill_Inspect_Verify_CrossProcess runs the recovery drill into a ledger
// and re-reads it: every drill agent's recovery/non-recovery record survived.
func TestDrill_Inspect_Verify_CrossProcess(t *testing.T) {
	dir := t.TempDir()

	drillOut, code := runCLI(t, "drill", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("drill exit=%d out=\n%s", code, drillOut)
	}
	if !strings.Contains(drillOut, "OVERALL: PASS") {
		t.Fatalf("drill did not pass:\n%s", drillOut)
	}

	inspOut, code := runCLI(t, "inspect", "--data-dir", dir)
	if code != 0 {
		t.Fatalf("inspect drill exit=%d:\n%s", code, inspOut)
	}
	if !strings.Contains(inspOut, "CHAIN VERIFY:    OK") {
		t.Fatalf("drill chain must verify:\n%s", inspOut)
	}
	// All four drill subjects were registered as agents.
	for _, want := range []string{"drl-2", "drl-3", "drl-4", "drl-5"} {
		if !strings.Contains(inspOut, want) {
			t.Errorf("inspect missing drill agent %s:\n%s", want, inspOut)
		}
	}
	// The non-recoverable L5 drill subject stays identity_revoked.
	if !regexp.MustCompile(`drl-5\s+level=L5\s+status=identity_revoked`).MatchString(inspOut) {
		t.Errorf("drl-5 must rebuild as L5/identity_revoked:\n%s", inspOut)
	}
	if vOut, vc := runCLI(t, "verify", "--data-dir", dir); vc != 0 {
		t.Fatalf("drill ledger verify exit=%d: %s", vc, vOut)
	}
}

// TestFaultInjection_TamperAndTornTail runs demo into a fresh dir, then:
//  1. flips one byte inside a complete published record → verify must be non-zero
//     and point at a hard tamper; repair-tail must refuse;
//  2. on a fresh dir, truncates the tail bytes → verify reports a torn tail;
//     repair-tail removes the incomplete line; verify returns to exit 0 with
//     one fewer record.
func TestFaultInjection_TamperAndTornTail(t *testing.T) {
	// --- 1. hard tamper on a complete record -------------------------------
	dir := t.TempDir()
	if out, code := runCLI(t, "demo", "--data-dir", dir); code != 0 {
		t.Fatalf("demo: %d %s", code, out)
	}
	seg := onlySegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the MIDDLE of the file (a complete record's payload).
	mid := len(data) / 2
	// Walk to a byte that actually changes so the test is meaningful.
	for data[mid] == data[mid]^0xFF {
		mid++
	}
	data[mid] ^= 0xFF
	if err := os.WriteFile(seg, data, 0o600); err != nil {
		t.Fatal(err)
	}

	vOut, vc := runCLI(t, "verify", "--data-dir", dir)
	if vc == 0 {
		t.Fatalf("verify must FAIL on a tampered record, got exit 0:\n%s", vOut)
	}
	if !strings.Contains(vOut, "INTEGRITY FAILURE") {
		t.Errorf("tamper output must carry INTEGRITY FAILURE, got:\n%s", vOut)
	}
	// repair-tail MUST refuse a hard tamper.
	rOut, rc := runCLI(t, "repair-tail", "--data-dir", dir)
	if rc == 0 {
		t.Errorf("repair-tail must refuse hard tamper, got exit 0:\n%s", rOut)
	}
	if !strings.Contains(rOut, "hard tamper") {
		t.Errorf("repair-tail must say 'hard tamper, refusing to repair':\n%s", rOut)
	}

	// --- 2. torn tail: truncate the last incomplete bytes -------------------
	dir2 := t.TempDir()
	if out, code := runCLI(t, "demo", "--data-dir", dir2); code != 0 {
		t.Fatalf("demo2: %d %s", code, out)
	}
	seg2 := onlySegment(t, dir2)
	data2, err := os.ReadFile(seg2)
	if err != nil {
		t.Fatal(err)
	}
	before := len(strings.Split(strings.TrimSuffix(string(data2), "\n"), "\n"))
	// Cut the final line in half → an unparseable tail.
	cutAt := len(data2) - 25
	if cutAt < 0 {
		t.Fatal("segment too small to truncate")
	}
	if err := os.WriteFile(seg2, data2[:cutAt], 0o600); err != nil {
		t.Fatal(err)
	}

	vOut2, vc2 := runCLI(t, "verify", "--data-dir", dir2)
	if vc2 == 0 {
		t.Fatalf("verify must report torn tail, got exit 0:\n%s", vOut2)
	}
	if !strings.Contains(vOut2, "INTEGRITY FAILURE") || !strings.Contains(vOut2, "torn") {
		t.Errorf("expected torn-tail failure:\n%s", vOut2)
	}

	rOut2, rc2 := runCLI(t, "repair-tail", "--data-dir", dir2)
	if rc2 != 0 {
		t.Fatalf("repair-tail must succeed on a genuine torn tail: %s", rOut2)
	}
	if !strings.Contains(rOut2, "torn tail 已修复") {
		t.Errorf("repair-tail output: %s", rOut2)
	}

	// After repair the ledger verifies and one record was dropped.
	vOut3, vc3 := runCLI(t, "verify", "--data-dir", dir2)
	if vc3 != 0 {
		t.Fatalf("post-repair verify must be exit 0: %s", vOut3)
	}
	after := len(strings.Split(strings.TrimSuffix(string(readSeg(t, dir2)), "\n"), "\n"))
	if after != before-1 {
		t.Errorf("record count after repair = %d want %d-1=%d", after, before, before-1)
	}
}

// onlySegment returns the single published segment file in dir (the demo writes
// exactly one).
func onlySegment(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "seg-*.log"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one segment in %s: matches=%v err=%v", dir, matches, err)
	}
	return matches[0]
}

func readSeg(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(onlySegment(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
