package audit

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func sampleEvent(typ domain.EventType) domain.Event {
	return domain.Event{
		Type:       typ,
		AgentID:    "agent-alpha",
		SentenceID: "sent-001",
		Actor:      "committee",
		Payload:    map[string]string{"level": "L2"},
		Timestamp:  time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC),
	}
}

func TestChainAppendLinksCorrectly(t *testing.T) {
	c := NewChain()
	e0 := c.Append(sampleEvent(domain.EventSentenced))
	e1 := c.Append(sampleEvent(domain.EventAllocationStopped))
	e2 := c.Append(sampleEvent(domain.EventResourceReclaimed))

	if c.Len() != 3 {
		t.Fatalf("Len = %d, want 3", c.Len())
	}
	if e0.Index != 0 || e0.PrevHash != GenesisPrevHash {
		t.Errorf("head event: index=%d prevHash=%q", e0.Index, e0.PrevHash)
	}
	if e1.Index != 1 || e1.PrevHash != e0.Hash {
		t.Errorf("second event must link to head hash")
	}
	if e2.PrevHash != e1.Hash {
		t.Errorf("third event must link to second hash")
	}
	for i, e := range []domain.Event{e0, e1, e2} {
		if e.Hash == "" {
			t.Errorf("event %d must carry a digest", i)
		}
	}
	if err := c.Verify(); err != nil {
		t.Fatalf("fresh chain must verify: %v", err)
	}
}

func TestChainGet(t *testing.T) {
	c := NewChain()
	c.Append(sampleEvent(domain.EventSentenced))
	if _, ok := c.Get(1); ok {
		t.Error("Get out of range must be false")
	}
	got, ok := c.Get(0)
	if !ok || got.Type != domain.EventSentenced {
		t.Errorf("Get(0) = %+v", got)
	}
}

func TestChainTamperDetected(t *testing.T) {
	c := NewChain()
	c.Append(sampleEvent(domain.EventSentenced))
	c.Append(sampleEvent(domain.EventAllocationStopped))
	c.Append(sampleEvent(domain.EventResourceReclaimed))

	cases := []struct {
		name string
		mut  func(e *domain.Event)
	}{
		{"type_changed", func(e *domain.Event) { e.Type = domain.EventIsolated }},
		{"payload_changed", func(e *domain.Event) { e.Payload["level"] = "L9" }},
		{"prevHash_changed", func(e *domain.Event) { e.PrevHash = "deadbeef" }},
		{"timestamp_changed", func(e *domain.Event) { e.Timestamp = e.Timestamp.Add(time.Hour) }},
		{"hash_overwritten", func(e *domain.Event) { e.Hash = "0000000000000000000000000000000000000000000000000000000000000000" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := c.Entries()
			tc.mut(&entries[1])
			err := VerifyEntries(entries)
			if err == nil {
				t.Fatalf("tampering %s must be detected", tc.name)
			}
			if !strings.Contains(err.Error(), "index 1") {
				t.Errorf("error must point at the tampered index 1, got: %v", err)
			}
		})
	}
}

// A realistic attacker recomputes the digest after editing a *middle* event:
// its own record then hashes cleanly, but the following entry's prevHash no
// longer matches, so the tamper is detected as a broken link at the next
// index. (The final entry has no forward link, which is why checkpoints are
// needed in a real deployment — noted, not solved, in this simulation.)
func TestChainRecomputedTamperDetectedAsBrokenLink(t *testing.T) {
	c := NewChain()
	c.Append(sampleEvent(domain.EventSentenced))
	c.Append(sampleEvent(domain.EventAllocationStopped))
	c.Append(sampleEvent(domain.EventResourceReclaimed))

	entries := c.Entries()
	entries[1].Payload["level"] = "L9"
	entries[1].Hash = hashOf(entries[1]) // attacker patches the digest too

	err := VerifyEntries(entries)
	if err == nil {
		t.Fatal("recomputed middle tamper must still break the link to the next entry")
	}
	if !strings.Contains(err.Error(), "broken chain link") || !strings.Contains(err.Error(), "index 2") {
		t.Errorf("expected a link error at index 2, got: %v", err)
	}
}

func TestChainConcurrentAppendRace(t *testing.T) {
	c := NewChain()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ev := sampleEvent(domain.EventReceiptAcked)
			ev.Payload = map[string]string{"n": fmt.Sprintf("%d", n)}
			c.Append(ev)
		}(i)
	}
	wg.Wait()
	if c.Len() != 64 {
		t.Fatalf("Len = %d, want 64", c.Len())
	}
	if err := c.Verify(); err != nil {
		t.Fatalf("concurrent chain must verify: %v", err)
	}
}
