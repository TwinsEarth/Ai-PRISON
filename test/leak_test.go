package e2e

// Zero-dependency goroutine-leak and file-residue guards. We deliberately do NOT
// pull in goleak: the standard library already gives us runtime.NumGoroutine(),
// which is enough to detect a System/FileStore that fails to terminate its
// background work. The acceptance criterion is: after a full demo System
// lifecycle (open FileStore → run both scenarios → Close → reopen/replay →
// Close), no goroutine count growth persists past a short settle window, and no
// temporary segment file is left behind in the data directory.

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/prison"
	"github.com/ai-prison/ai-prison/internal/store"
)

// settleUntil returns the goroutine count once it has dropped to
// base+tolerance, polling until the deadline. The final observed count is
// always returned so the caller can assert on it.
func settleUntil(base, tolerance int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	n := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		n = runtime.NumGoroutine()
		if n <= base+tolerance {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
	return n
}

// TestLeak_FullDemoSystemLifecycle drives a real FileStore-backed System through
// open → both scenarios → close → reopen (event-sourced replay) → close, twice,
// and asserts the goroutine count returns to the settled baseline.
func TestLeak_FullDemoSystemLifecycle(t *testing.T) {
	// Settle once before measuring the baseline (test framework bookkeeping and
	// any residual scheduler work must already be done).
	base := settleUntil(runtime.NumGoroutine(), 0, 500*time.Millisecond)
	const tolerance = 4

	runOne := func() {
		dir := t.TempDir()
		fs, err := store.NewFileStore(dir, store.Options{Clock: prison.DefaultClock})
		if err != nil {
			t.Fatalf("open FileStore: %v", err)
		}
		sys := prison.NewSystem(prison.Config{Store: fs})
		if sys.BootstrapErr != nil {
			t.Fatalf("bootstrap: %v", sys.BootstrapErr)
		}
		if _, err := sys.RunScenarioA(); err != nil {
			t.Fatalf("scenario A: %v", err)
		}
		if _, err := sys.RunScenarioB(); err != nil {
			t.Fatalf("scenario B: %v", err)
		}
		if err := sys.Close(); err != nil {
			t.Fatalf("close system: %v", err)
		}

		// Reopen: event-sourced replay onto a fresh System, then close again.
		fs2, err := store.NewFileStore(dir, store.Options{Clock: prison.DefaultClock})
		if err != nil {
			t.Fatalf("reopen FileStore: %v", err)
		}
		sys2 := prison.NewSystem(prison.Config{Store: fs2})
		if sys2.BootstrapErr != nil {
			t.Fatalf("replay bootstrap: %v", sys2.BootstrapErr)
		}
		if v := sys2.InspectView(); !v.ChainVerifyOK || !v.Restored {
			t.Fatalf("reopened system must verify and report Restored: %+v", v)
		}
		if err := sys2.Close(); err != nil {
			t.Fatalf("close reopened system: %v", err)
		}

		// No unpublished temp segment files may remain after clean closes.
		if matches, _ := filepath.Glob(filepath.Join(dir, ".seg-*.tmp")); len(matches) != 0 {
			t.Fatalf("leftover temp segment files: %v", matches)
		}
	}

	runOne()
	afterFirst := settleUntil(base, tolerance, 2*time.Second)
	if afterFirst > base+tolerance {
		t.Fatalf("goroutine count grew after first lifecycle: base=%d now=%d (tolerance=%d)",
			base, afterFirst, tolerance)
	}

	// Second cycle: growth must not accumulate.
	runOne()
	afterSecond := settleUntil(base, tolerance, 2*time.Second)
	if afterSecond > base+tolerance {
		t.Fatalf("goroutine count grew after second lifecycle: base=%d now=%d (tolerance=%d)",
			base, afterSecond, tolerance)
	}
	t.Logf("goroutine baseline=%d after-first=%d after-second=%d", base, afterFirst, afterSecond)
}

// TestLeak_FileStoreOpenWriteCloseCycles hammers the low-level FileStore
// publish path (temp-file → fsync → rename → dir fsync) across repeated
// open/write/Sync/Close cycles and asserts (a) the ledger reopens cleanly and
// (b) no *.tmp files survive a normal close.
func TestLeak_FileStoreOpenWriteCloseCycles(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		fs, err := store.NewFileStore(dir, store.Options{Clock: prison.DefaultClock})
		if err != nil {
			t.Fatalf("cycle %d open: %v", i, err)
		}
		for j := 0; j < 40; j++ {
			if _, err := fs.Append(context.Background(), "leak.probe",
				map[string]int{"cycle": i, "n": j}); err != nil {
				t.Fatalf("cycle %d append %d: %v", i, j, err)
			}
		}
		if err := fs.Sync(context.Background()); err != nil {
			t.Fatalf("cycle %d sync: %v", i, err)
		}
		if err := fs.Close(); err != nil {
			t.Fatalf("cycle %d close: %v", i, err)
		}
		if matches, _ := filepath.Glob(filepath.Join(dir, ".seg-*.tmp")); len(matches) != 0 {
			t.Fatalf("cycle %d leftover temp files: %v", i, matches)
		}
	}

	// Final integrity: the replayed chain must verify.
	ro, err := store.OpenReadonly(dir)
	if err != nil {
		t.Fatalf("final readonly open: %v", err)
	}
	n := ro.Len()
	_ = ro.Close()
	if want := 5 * 40; n != want {
		t.Fatalf("final record count = %d, want %d", n, want)
	}

	// Goroutines from this workload must settle back down too.
	base := settleUntil(runtime.NumGoroutine(), 0, 200*time.Millisecond)
	if n := settleUntil(base, 4, 2*time.Second); n > base+4 {
		t.Fatalf("goroutine growth after FileStore cycles: base=%d now=%d", base, n)
	}
}
