package revocation

import (
	"fmt"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// BenchmarkIsUsableRevokedHit measures the cardinal gate: a still-in-window
// credential must be rejected by the global revocation set before the validity
// window is even consulted. This is the read path every access decision takes.
func BenchmarkIsUsableRevokedHit(b *testing.B) {
	set := NewSet()
	cred := domain.Credential{
		ID:        "cred-bench-1",
		AgentID:   "agent-bench",
		Kind:      domain.CredentialResource,
		State:     domain.CredentialActive,
		IssuedAt:  time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	set.Revoke(Entry{ID: cred.ID, AgentID: cred.AgentID, Kind: cred.Kind}, nil)
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cred.IsUsable(now, set) {
			b.Fatal("revoked credential must not be usable")
		}
	}
}

// BenchmarkIsRevokedLargeSet measures a single IsRevoked map hit against a
// 100k-entry revocation set (the worst realistic read).
func BenchmarkIsRevokedLargeSet(b *testing.B) {
	const n = 100_000
	set := NewSet()
	target := fmt.Sprintf("cred-%d", n/2)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cred-%d", i)
		set.Revoke(Entry{ID: id, AgentID: domain.AgentID(fmt.Sprintf("agent-%d", i)), Kind: domain.CredentialResource}, nil)
	}
	if !set.IsRevoked(target) {
		b.Fatalf("target %s must be revoked", target)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !set.IsRevoked(target) {
			b.Fatal("miss")
		}
	}
}

// BenchmarkIsRevokedMiss measures a negative lookup in the same 100k set.
func BenchmarkIsRevokedLargeSetMiss(b *testing.B) {
	const n = 100_000
	set := NewSet()
	for i := 0; i < n; i++ {
		set.Revoke(Entry{ID: fmt.Sprintf("cred-%d", i), AgentID: "a", Kind: domain.CredentialResource}, nil)
	}
	miss := "cred-not-present"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if set.IsRevoked(miss) {
			b.Fatal("must miss")
		}
	}
}
