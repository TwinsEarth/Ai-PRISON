package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAndClose creates a fresh file store, appends n records of the given
// options, closes it and returns the data directory plus the record count.
func writeAndClose(t *testing.T, dir string, opts Options, n int, kinds []string) {
	t.Helper()
	s, err := NewFileStore(dir, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < n; i++ {
		k := KindAuditEvent
		if i < len(kinds) {
			k = kinds[i]
		}
		if _, err := s.Append(context.Background(), k, testPayload{
			AgentID: "agent-" + itoa(i),
			Level:   "L3",
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestStaleTempFileDiscardedOnOpen(t *testing.T) {
	dir := t.TempDir()
	writeAndClose(t, dir, Options{SegmentSize: 2}, 3, nil)

	// Simulate a crash after creating the temp file but before rename.
	stale := filepath.Join(dir, ".seg-9999999999.tmp")
	if err := os.WriteFile(stale, []byte("{partial\n"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	s, err := NewFileStore(dir, Options{SegmentSize: 2})
	if err != nil {
		t.Fatalf("reopen with stale temp: %v", err)
	}
	defer s.Close()
	if s.CleanedTempFiles() != 1 {
		t.Fatalf("cleaned=%d want 1", s.CleanedTempFiles())
	}
	if s.Len() != 3 {
		t.Fatalf("records=%d want 3", s.Len())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale temp file was not removed")
	}
}

func TestCrashBeforeSyncDropsOnlyUnpublishedRecords(t *testing.T) {
	dir := t.TempDir()
	// Batching, large segment, no explicit Close/Sync: emulate a kill.
	s, err := NewFileStore(dir, Options{SegmentSize: 1000})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// First publish a committed batch.
	for i := 0; i < 3; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "committed"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := s.Sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// Then buffer more records that never get flushed (crash before Sync).
	for i := 0; i < 5; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "lost"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Abandon WITHOUT Close: a new process opens the same directory.
	s2, err := NewFileStore(dir, Options{SegmentSize: 1000})
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer s2.Close()
	if s2.Len() != 3 {
		t.Fatalf("after crash records=%d want 3 (unpublished batch discarded)", s2.Len())
	}
	var recs []Record
	if err := s2.Replay(context.Background(), func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := Verify(recs); err != nil {
		t.Fatalf("committed ledger not intact after crash: %v", err)
	}
}

func TestSyncEveryAppendSurvivesKill(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "durable"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Abandon without Close; each record was already durable.
	s2, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Len() != 6 {
		t.Fatalf("records=%d want 6", s2.Len())
	}
	var recs []Record
	_ = s2.Replay(context.Background(), func(r Record) error { recs = append(recs, r); return nil })
	if err := Verify(recs); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestTornTailDetectedAndRepaired(t *testing.T) {
	dir := t.TempDir()
	// One segment containing 3 records (batch fully synced).
	writeAndClose(t, dir, Options{SegmentSize: 100}, 3, nil)
	seg := filepath.Join(dir, "seg-0000000000.log")
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read seg: %v", err)
	}
	// Cut the last 12 bytes: removes the newline plus the end of the last line.
	if len(data) <= 12 {
		t.Fatalf("segment too short: %d", len(data))
	}
	if err := os.WriteFile(seg, data[:len(data)-12], 0o600); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	_, err = NewFileStore(dir, Options{SegmentSize: 100})
	if err == nil {
		t.Fatal("expected torn-tail error on open")
	}
	if !IsTornTail(err) {
		t.Fatalf("expected torn tail, got %v", err)
	}
	if IsTamper(err) {
		t.Fatalf("a torn tail must not be classified as hard tamper: %v", err)
	}
	removed, repaired, err := RepairTornTail(dir)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if removed == 0 || repaired == "" {
		t.Fatalf("repair removed=%d file=%q", removed, repaired)
	}
	s, err := NewFileStore(dir, Options{SegmentSize: 100})
	if err != nil {
		t.Fatalf("reopen after repair: %v", err)
	}
	defer s.Close()
	if s.Len() != 2 {
		t.Fatalf("after repair records=%d want 2 (torn tail removed)", s.Len())
	}
	var recs []Record
	_ = s.Replay(context.Background(), func(r Record) error { recs = append(recs, r); return nil })
	if err := Verify(recs); err != nil {
		t.Fatalf("repaired ledger does not verify: %v", err)
	}
}

func TestTamperedCompleteRecordIsHardError(t *testing.T) {
	dir := t.TempDir()
	// One record per segment so the tampered record is not the last segment.
	s, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target := "TARGET"
	if _, err := s.Append(context.Background(), "marker", map[string]string{"note": target}); err != nil {
		t.Fatalf("append target: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "after"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The target record is the first segment (non-last): tamper its payload.
	first := filepath.Join(dir, "seg-0000000000.log")
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), target) {
		t.Fatal("target marker not found in segment")
	}
	bad := strings.Replace(string(data), target, "TAMPER", 1) // same length
	if err := os.WriteFile(first, []byte(bad), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	_, err = NewFileStore(dir, Options{SyncEveryAppend: true})
	if err == nil {
		t.Fatal("expected integrity error on tampered ledger")
	}
	if !IsTamper(err) {
		t.Fatalf("expected hard tamper error, got %T: %v", err, err)
	}
	if IsTornTail(err) {
		t.Fatalf("tampered complete record must not be reparable as a torn tail: %v", err)
	}
	// RepairTornTail must not touch a complete record.
	removed, _, rerr := RepairTornTail(dir)
	if rerr != nil {
		t.Fatalf("repair probe: %v", rerr)
	}
	if removed != 0 {
		t.Fatalf("RepairTornTail removed %d bytes from a complete record", removed)
	}
}

func TestCorruptHashIsHardError(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "a"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Flip the first record's stored hash hex digit in a non-last segment.
	first := filepath.Join(dir, "seg-0000000000.log")
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	idx := strings.Index(string(data), `"hash":"`)
	if idx < 0 {
		t.Fatal("hash field not found")
	}
	pos := idx + len(`"hash":"`)
	b := []byte(data)
	if b[pos] == '0' {
		b[pos] = '1'
	} else {
		b[pos] = '0'
	}
	if err := os.WriteFile(first, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = NewFileStore(dir, Options{SyncEveryAppend: true})
	if err == nil || !IsTamper(err) {
		t.Fatalf("expected hard tamper error, got %v", err)
	}
}
