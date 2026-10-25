package prison

import (
	"testing"

	"github.com/ai-prison/ai-prison/internal/store"
)

// BenchmarkRunScenarioAInMemory drives the full L3 recoverable journey
// (seed → evidence → two-phase vote → enforce → appeal upheld → finalize) on a
// fresh in-memory System per iteration.
func BenchmarkRunScenarioAInMemory(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sys := NewSystem(Config{})
		if _, err := sys.RunScenarioA(); err != nil {
			b.Fatal(err)
		}
		_ = sys.Close()
	}
}

// BenchmarkRunScenarioAFileStore drives the same journey with a real crash-safe
// FileStore on disk (fresh temp dir per iteration, fsync on Close).
func BenchmarkRunScenarioAFileStore(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dir := b.TempDir()
		fs, err := store.NewFileStore(dir, store.Options{SegmentSize: 64})
		if err != nil {
			b.Fatal(err)
		}
		sys := NewSystem(Config{Store: fs})
		if _, err := sys.RunScenarioA(); err != nil {
			b.Fatal(err)
		}
		if err := sys.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
