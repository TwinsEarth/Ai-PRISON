package resource

import (
	"testing"

	"github.com/ai-prison/ai-prison/internal/revocation"
)

// BenchmarkDepriveFullSequence drives the complete five-step resource
// deprivation (revoke → stop allocation → snapshot seal → gradual reclaim over
// DefaultSteps rungs → freeze memory → cut network → forced receipts) per
// iteration.
func BenchmarkDepriveFullSequence(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		set := revocation.NewSet()
		eng := NewEngine(Config{
			Backend:   NewInMemoryBackend(set),
			Snapshots: NewMemSnapshotStore(),
		})
		// Re-allocate so the backend has a live quota to reclaim.
		_ = eng.Backend.Allocate(baseQuota())
		_, err := eng.Deprive(DepriveInput{
			SentenceID:   "sentence-bench",
			AgentID:      testAgent,
			CredentialID: "cred-resource-1",
			Reason:       "bench: gradual deprivation",
			HoldingNodes: []string{"node-alpha", "node-beta"},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
