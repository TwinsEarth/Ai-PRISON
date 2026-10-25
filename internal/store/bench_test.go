package store

import (
	"context"
	"testing"
)

// samplePayload is a representative event payload for append benchmarks.
func samplePayload(i int) map[string]string {
	return map[string]string{
		"seq":       itoa(i),
		"agentId":   "agent-bench",
		"operation": "resource.reclaimed",
		"node":      "node-alpha",
		"detail":    "simulated bench record payload",
	}
}

// BenchmarkAppendSyncEveryAppend measures the strongest-durability mode: one
// fsync + atomic directory rename PER record. This is the upper bound on
// per-record fsync cost.
func BenchmarkAppendSyncEveryAppend(b *testing.B) {
	dir := b.TempDir()
	fs, err := NewFileStore(dir, Options{SyncEveryAppend: true})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fs.Append(ctx, "audit.event", samplePayload(i)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	_ = fs.Close()
}

// BenchmarkAppendBatched measures the default durability mode: records are
// buffered in memory and only published/fsynced once per SegmentSize (256),
// with a final Sync at Close.
func BenchmarkAppendBatched(b *testing.B) {
	dir := b.TempDir()
	fs, err := NewFileStore(dir, Options{SegmentSize: 256})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fs.Append(ctx, "audit.event", samplePayload(i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := fs.Sync(ctx); err != nil {
		b.Fatal(err)
	}
	b.StopTimer()
	_ = fs.Close()
}

// BenchmarkReplayVerify measures reopening a ledger and re-verifying the whole
// hash chain: open FileStore already replays + verifies on construction.
func BenchmarkReplayVerify(b *testing.B) {
	const n = 10_000
	dir := b.TempDir()
	setup, err := NewFileStore(dir, Options{SegmentSize: 256})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if _, err := setup.Append(ctx, "audit.event", samplePayload(i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := setup.Close(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reopened, err := NewFileStore(dir, Options{SegmentSize: 256})
		if err != nil {
			b.Fatal(err)
		}
		if reopened.Len() != n {
			b.Fatalf("replayed %d, want %d", reopened.Len(), n)
		}
		_ = reopened.Close()
	}
}
