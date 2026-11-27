package store

// Retention tests: the whole ledger used to stay resident (records slice) and
// startup parsed every segment. These guard the bounded hot window and the
// cold-segment streaming replay that replaced it.

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// testDir isolates retention tests from t.TempDir on hosts whose sandbox allows
// writes but not deletes; set AIPRISON_TEST_DIR to a scratch directory there.
func testDir(t *testing.T) (string, func()) {
	t.Helper()
	if base := os.Getenv("AIPRISON_TEST_DIR"); base != "" {
		dir, err := os.MkdirTemp(base, "retain-")
		if err != nil {
			t.Fatalf("scratch dir: %v", err)
		}
		return dir, func() { _ = os.RemoveAll(dir) }
	}
	dir := t.TempDir()
	return dir, func() {}
}

func TestRetainedWindowStaysBounded(t *testing.T) {
	dir, cleanup := testDir(t)
	defer cleanup()

	const (
		total     = 2000
		segSize   = 32
		retain    = 2
		wantInMem = retain * segSize
	)
	ctx := context.Background()
	s, err := NewFileStore(dir, Options{
		SegmentSize:    segSize,
		RetainSegments: retain,
		Clock:          fixedClock(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	for i := 0; i < total; i++ {
		if _, err := s.Append(ctx, KindAuditEvent, map[string]string{"i": strconv.Itoa(i)}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	st := s.Stats()
	if st.TotalRecords != total {
		t.Fatalf("TotalRecords=%d want %d", st.TotalRecords, total)
	}
	if st.InMemory != wantInMem {
		t.Fatalf("InMemory=%d want %d (hot window must stay bounded)", st.InMemory, wantInMem)
	}
	if st.BaseSeq != uint64(total-wantInMem) {
		t.Fatalf("BaseSeq=%d want %d", st.BaseSeq, total-wantInMem)
	}

	// Cold prefix must still replay, in order, from disk.
	seen := 0
	var lastSeq uint64
	err = s.Replay(ctx, func(r Record) error {
		if seen > 0 && r.Seq != lastSeq+1 {
			t.Fatalf("replay gap at %d: seq=%d", seen, r.Seq)
		}
		lastSeq = r.Seq
		seen++
		return nil
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if seen != total {
		t.Fatalf("replay visited %d records want %d", seen, total)
	}

	// Reopen must reload only the tail window but still expose the full ledger.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	re, err := NewFileStore(dir, Options{SegmentSize: segSize, RetainSegments: retain})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()
	st2 := re.Stats()
	if st2.TotalRecords != total || st2.InMemory != wantInMem {
		t.Fatalf("reopen stats total=%d inmem=%d want %d/%d", st2.TotalRecords, st2.InMemory, total, wantInMem)
	}
	if got := re.Len(); got != total {
		t.Fatalf("Len=%d want %d", got, total)
	}
}

func TestRetainZeroKeepsWholeLedger(t *testing.T) {
	dir, cleanup := testDir(t)
	defer cleanup()

	ctx := context.Background()
	s, err := NewFileStore(dir, Options{SegmentSize: 8, RetainSegments: 0})
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	defer s.Close()
	for i := 0; i < 80; i++ {
		if _, err := s.Append(ctx, KindAuditEvent, i); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	st := s.Stats()
	if st.InMemory != 80 || st.BaseSeq != 0 {
		t.Fatalf("InMemory=%d BaseSeq=%d want 80/0 (0 = keep all)", st.InMemory, st.BaseSeq)
	}
}
