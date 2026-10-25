package governance

import (
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// benchPipeline drives the seven-stage two-phase pipeline and returns any error
// instead of calling Fatal, so it can be used from benchmarks.
func benchPipeline(g *Governance, v domain.Violation) error {
	c, err := g.OpenCase(v)
	if err != nil {
		return err
	}
	if _, err := g.AdmitEvidence(c.ID, []domain.Charge{
		{ViolationType: v.Type, Description: "proven", Evidence: []string{"evt:1"}},
	}); err != nil {
		return err
	}
	if _, err := g.ConfirmBaseline(c.ID, allSeats); err != nil {
		return err
	}
	phases := []struct {
		phase   domain.VotePhase
		approve []bool
	}{
		{domain.PhaseDeprive, []bool{true, true, true, false}},
		{domain.PhaseGrade, []bool{true, true, true, false}},
	}
	for _, ph := range phases {
		for i, approve := range ph.approve {
			if err := g.RecordVote(c.ID, vote(c.ID, "v", allSeats[i], ph.phase, approve, "")); err != nil {
				return err
			}
		}
		if ph.phase == domain.PhaseDeprive {
			if _, _, err := g.CompleteDeprive(c.ID); err != nil {
				return err
			}
		} else {
			if _, _, err := g.CompleteGrade(c.ID); err != nil {
				return err
			}
		}
	}
	_, err = g.IssueSentence(c.ID, validRecords())
	return err
}

// BenchmarkFullCaseToVerdict drives the complete seven-stage two-phase pipeline
// on a fresh Governance per iteration.
func BenchmarkFullCaseToVerdict(b *testing.B) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	v := domain.Violation{
		ID:         "viol-bench",
		AgentID:    "agent-bench",
		DID:        "did:agent-bench",
		Type:       domain.ViolationSandboxEscapeAttempt,
		Signals:    []string{"s:1"},
		Source:     "bench",
		DetectedAt: now,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := New(Config{})
		if err := benchPipeline(g, v); err != nil {
			b.Fatal(err)
		}
	}
}
