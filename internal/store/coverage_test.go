package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAccessorsCovered exercises the small accessors that the conformance suite
// does not otherwise call: FileStore.Path / Persisted and MemoryStore.Path.
func TestAccessorsCovered(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if fs.Path() != dir {
		t.Errorf("Path() = %q, want %q", fs.Path(), dir)
	}
	if _, err := fs.Append(context.Background(), "kind.a", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := fs.Persisted(); got != 1 {
		t.Errorf("Persisted() after SyncEveryAppend append = %d, want 1", got)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ms := NewMemoryStore()
	if ms.Path() != "" {
		t.Errorf("MemoryStore.Path() = %q, want empty", ms.Path())
	}
	if ms.Len() != 0 {
		t.Errorf("fresh MemoryStore.Len() = %d, want 0", ms.Len())
	}
}

// TestNewMemoryStoreWithClock verifies the injected clock drives record
// timestamps deterministically and that a nil clock falls back to time.Now.
func TestNewMemoryStoreWithClock(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ms := NewMemoryStoreWithClock(func() time.Time { return fixed })
	rec, err := ms.Append(context.Background(), "k", nil)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !rec.At.Equal(fixed) {
		t.Errorf("record At = %v, want %v", rec.At, fixed)
	}

	nilClock := NewMemoryStoreWithClock(nil)
	if _, err := nilClock.Append(context.Background(), "k", nil); err != nil {
		t.Fatalf("Append with nil clock: %v", err)
	}
}

// TestMarshalPayloadEdges covers the branches not hit by the conformance suite:
// nil payload, empty RawMessage, invalid RawMessage and a non-JSON type.
func TestMarshalPayloadEdges(t *testing.T) {
	if b, err := marshalPayload(nil); err != nil || string(b) != "null" {
		t.Errorf("marshalPayload(nil) = %q, %v; want null, nil", b, err)
	}
	if b, err := marshalPayload(json.RawMessage(nil)); err != nil || string(b) != "null" {
		t.Errorf("marshalPayload(empty RawMessage) = %q, %v; want null, nil", b, err)
	}
	if _, err := marshalPayload(json.RawMessage("{not json")); err == nil {
		t.Error("invalid RawMessage must error")
	}
	if b, err := marshalPayload(json.RawMessage(`{"a":1}`)); err != nil || string(b) != `{"a":1}` {
		t.Errorf("valid RawMessage must pass through, got %q %v", b, err)
	}
	if _, err := marshalPayload(make(chan struct{})); err == nil {
		t.Error("non-marshalable payload must error")
	}
}

// TestRepairTornTailNoOpPaths covers the early-return branches of RepairTornTail:
// empty directory, a segment already ending in a clean newline, and a single
// complete line that parses.
func TestRepairTornTailNoOpPaths(t *testing.T) {
	// Empty dir: no segments, nothing to repair.
	if n, f, err := RepairTornTail(t.TempDir()); err != nil || n != 0 || f != "" {
		t.Errorf("RepairTornTail(empty) = %d, %q, %v; want 0,\"\",nil", n, f, err)
	}

	dir := t.TempDir()
	// A clean segment that ends with a newline: no torn tail.
	clean := filepath.Join(dir, "seg-0000000000.log")
	if err := os.WriteFile(clean, []byte("{\"seq\":0,\"hash\":\"x\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, _, err := RepairTornTail(dir); err != nil || n != 0 {
		t.Errorf("RepairTornTail(clean) = %d, %v; want 0", n, err)
	}

	// A single-line file (no newline) that DOES parse: not a torn tail.
	oneLine := filepath.Join(dir, "seg-0000000001.log")
	if err := os.WriteFile(oneLine, []byte("{\"seq\":1,\"hash\":\"y\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, _, err := RepairTornTail(dir); err != nil || n != 0 {
		t.Errorf("RepairTornTail(one-line-valid) = %d, %v; want 0", n, err)
	}
}

// TestAppendSyncHonorsCancelledContext verifies the Store contract that a
// cancelled context aborts Append/Sync without mutating the ledger. This is the
// regression for the previously-ignored ctx parameter.
func TestAppendSyncHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mem := NewMemoryStore()
	if _, err := mem.Append(ctx, "k", nil); err != context.Canceled {
		t.Errorf("MemoryStore.Append(cancelled) = %v, want context.Canceled", err)
	}
	if mem.Len() != 0 {
		t.Errorf("cancelled Append must not append; Len=%d", mem.Len())
	}

	dir := t.TempDir()
	fs, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if _, err := fs.Append(ctx, "k", nil); err != context.Canceled {
		t.Errorf("FileStore.Append(cancelled) = %v, want context.Canceled", err)
	}
	if err := fs.Sync(ctx); err != context.Canceled {
		t.Errorf("FileStore.Sync(cancelled) = %v, want context.Canceled", err)
	}
	if fs.Len() != 0 {
		t.Errorf("cancelled FileStore Append must not append; Len=%d", fs.Len())
	}
	// A live context must still work after a cancelled one.
	if _, err := fs.Append(context.Background(), "k", map[string]int{"n": 1}); err != nil {
		t.Fatalf("Append with live ctx: %v", err)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
