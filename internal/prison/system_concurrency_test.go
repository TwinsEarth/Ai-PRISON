package prison

// Concurrency contract tests. The orchestration contract is documented on
// sysState (state.go): public System methods may run concurrently as long as
// they act on different agents and never share a *Journey. These tests hammer
// the lock-bearing projections and the replay-side read path; run them with
// `go test -race ./internal/prison` on a host that supports cgo/race.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// TestSysStateConcurrentAccess hammers every projection accessor with
// overlapping writers and readers. It is the direct guard for the four maps
// that used to be lock-free (runtimeStatus/freezeHooks/agentReputation/proj).
func TestSysStateConcurrentAccess(t *testing.T) {
	st := newSysState()
	const writers, readers, iterations = 8, 8, 400

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := domain.AgentID(fmt.Sprintf("agent-w%d", w))
			for i := 0; i < iterations; i++ {
				st.setStatus(id, domain.StatusActive)
				st.setReputation(id, i)
				st.markFrozen(id, i)
				st.updateProject(id, func(p *RestoredAgent) {
					p.Reputation = i
					p.Status = domain.StatusActive
				})
				st.incCase()
				st.incSentence()
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := domain.AgentID(fmt.Sprintf("agent-w%d", i%writers))
				_ = st.statusOf(id)
				_ = st.reputationOf(id)
				_, _ = st.frozenOrdinal(id)
				_, _ = st.projectCopy(id)
				st.eachStatus(func(domain.AgentID, domain.AgentStatus, int, string) {})
				st.eachProject(func(domain.AgentID, RestoredAgent) {})
				_, _ = st.counts()
			}
		}(r)
	}
	wg.Wait()

	cases, sentences := st.counts()
	if want := writers * iterations; cases != want || sentences != want {
		t.Fatalf("counters lost updates: cases=%d sentences=%d want=%d", cases, sentences, want)
	}
	for w := 0; w < writers; w++ {
		id := domain.AgentID(fmt.Sprintf("agent-w%d", w))
		if got := st.statusOf(id); got != domain.StatusActive {
			t.Fatalf("%s status=%q", id, got)
		}
		if got := st.reputationOf(id); got != iterations-1 {
			t.Fatalf("%s reputation=%d want %d", id, got, iterations-1)
		}
		if got, ok := st.frozenOrdinal(id); !ok || got != iterations-1 {
			t.Fatalf("%s frozen=%d,%v want %d,true", id, got, ok, iterations-1)
		}
	}
}

// TestSystemConcurrentSeed drives the real orchestrator from parallel
// goroutines (one agent per goroutine, one Journey per agent) while a reader
// loop keeps calling the public read paths. It fails on lost projections and
// is meant to be paired with -race.
func TestSystemConcurrentSeed(t *testing.T) {
	sys := NewSystem(Config{})
	const agents = 8

	stop := make(chan struct{})
	var seedWG, readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
				sys.InspectView()
				_ = sys.RuntimeStatus("agent-c0")
			}
		}
	}()

	ids := make([]domain.AgentID, agents)
	for i := 0; i < agents; i++ {
		ids[i] = domain.AgentID(fmt.Sprintf("agent-c%d", i))
	}
	for i := 0; i < agents; i++ {
		seedWG.Add(1)
		go func(i int) {
			defer seedWG.Done()
			now := sys.Clock()
			a := domain.Agent{
				ID: ids[i],
				Card: domain.AgentCard{
					DID:        domain.DID("did:ai-prison:" + string(ids[i])),
					Name:       "concurrency-worker",
					ValidFrom:  now.Add(-time.Hour),
					ValidUntil: now.Add(time.Hour),
				},
			}
			a.Principal, a.Deployer, a.ModelProviderParty = parties(
				"hp-c", "Principal C", "hd-c", "Deploy C", "hm-c", "Model C")
			j := sys.NewJourney("concurrency: parallel seed")
			if err := sys.SeedAgent(j, SeedSpec{
				Agent:       a,
				Nodes:       []string{"node-c"},
				Fingerprint: "fp-concurrency",
			}); err != nil {
				t.Errorf("seed %s: %v", ids[i], err)
			}
		}(i)
	}
	seedWG.Wait()
	close(stop)
	readerWG.Wait()

	for i := 0; i < agents; i++ {
		if got := sys.RuntimeStatus(ids[i]); got != domain.StatusActive {
			t.Fatalf("%s status=%q want active", ids[i], got)
		}
		if p, ok := sys.state.projectCopy(ids[i]); !ok || p.Status != domain.StatusActive {
			t.Fatalf("%s projection missing or stale: %+v ok=%v", ids[i], p, ok)
		}
	}
	if view := sys.InspectView(); len(view.Agents) != agents {
		t.Fatalf("inspect agents=%d want %d", len(view.Agents), agents)
	}
}
