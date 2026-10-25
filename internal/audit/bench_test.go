package audit

import (
	"fmt"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func benchEvent(i int) domain.Event {
	return domain.Event{
		Type:       domain.EventResourceReclaimed,
		AgentID:    domain.AgentID(fmt.Sprintf("agent-%d", i%64)),
		SentenceID: fmt.Sprintf("sentence-%d", i%16),
		Actor:      "bench",
		Payload: map[string]string{
			"rung":      fmt.Sprintf("%d", i%8),
			"cpuMillis": fmt.Sprintf("%d", 1000-i%100),
			"simulated": "true",
		},
		Timestamp: time.Unix(int64(i), 0).UTC(),
	}
}

// BenchmarkChainAppend measures the cost of stamping index/prevHash/hash on one
// event (the O(1) append path).
func BenchmarkChainAppend(b *testing.B) {
	c := NewChain()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Append(benchEvent(i))
	}
}

// BenchmarkVerify_N measures full-chain verification (recompute every digest and
// re-check every link) at three scales.
func BenchmarkVerify_1k(b *testing.B)   { benchVerifyN(b, 1_000) }
func BenchmarkVerify_10k(b *testing.B)  { benchVerifyN(b, 10_000) }
func BenchmarkVerify_100k(b *testing.B) { benchVerifyN(b, 100_000) }

func benchVerifyN(b *testing.B, n int) {
	c := NewChain()
	for i := 0; i < n; i++ {
		c.Append(benchEvent(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Verify(); err != nil {
			b.Fatal(err)
		}
	}
}
