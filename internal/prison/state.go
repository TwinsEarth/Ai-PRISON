package prison

import (
	"sync"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// sysState owns every mutable runtime-side map of a System once it is built.
//
// CONCURRENCY CONTRACT
//
// The orchestrator's public methods (SeedAgent / RunScenario / Enforce / Appeal
// / Rehabilitate / Finalize / Inspect) may be called from different goroutines
// as long as they operate on different agents; the shared runtime projections
// below are always accessed through sysState, which serialises them. A single
// System still serialises ledger appends and chain appends internally, so two
// goroutines enforcing the same agent will get a consistent ledger, but the
// per-agent Journey object itself is NOT safe for concurrent use — callers must
// not share one *Journey across goroutines.
//
// This is the contract the DSH host loader and any future parallel driver rely
// on; it is exercised by TestSystemConcurrentState under -race.
type sysState struct {
	mu       sync.RWMutex
	frozen   map[domain.AgentID]int
	status   map[domain.AgentID]domain.AgentStatus
	rep      map[domain.AgentID]int
	proj     map[domain.AgentID]*RestoredAgent
	cases    int
	verbs    int
}

func newSysState() *sysState {
	return &sysState{
		frozen: make(map[domain.AgentID]int),
		status: make(map[domain.AgentID]domain.AgentStatus),
		rep:    make(map[domain.AgentID]int),
		proj:   make(map[domain.AgentID]*RestoredAgent),
	}
}

func (s *sysState) setStatus(a domain.AgentID, st domain.AgentStatus) {
	s.mu.Lock()
	s.status[a] = st
	s.mu.Unlock()
}

func (s *sysState) statusOf(a domain.AgentID) domain.AgentStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status[a]
}

func (s *sysState) setReputation(a domain.AgentID, rep int) {
	s.mu.Lock()
	s.rep[a] = rep
	s.mu.Unlock()
}

func (s *sysState) reputationOf(a domain.AgentID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rep[a]
}

func (s *sysState) markFrozen(a domain.AgentID, ordinal int) {
	s.mu.Lock()
	s.frozen[a] = ordinal
	s.mu.Unlock()
}

func (s *sysState) frozenOrdinal(a domain.AgentID) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.frozen[a]
	return v, ok
}

// setProject installs (and stores a private copy of) an agent projection.
func (s *sysState) setProject(a domain.AgentID, p *RestoredAgent) {
	if p == nil {
		return
	}
	cp := *p
	s.mu.Lock()
	s.proj[a] = &cp
	s.mu.Unlock()
}

// projectCopy returns a detached copy so callers never mutate shared state.
func (s *sysState) projectCopy(a domain.AgentID) (RestoredAgent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.proj[a]
	if !ok {
		return RestoredAgent{}, false
	}
	return *p, true
}

// updateProject mutates the projection in place under the write lock, creating
// the entry when it does not exist yet (mirrors the replay/live upsert paths).
func (s *sysState) updateProject(a domain.AgentID, fn func(p *RestoredAgent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.proj[a]
	if !ok {
		p = &RestoredAgent{AgentID: a}
		s.proj[a] = p
	}
	fn(p)
}

// eachStatus snapshots the status/reputation/level triple for each known agent.
func (s *sysState) eachStatus(fn func(a domain.AgentID, st domain.AgentStatus, rep int, level string)) {
	s.mu.RLock()
	type row struct {
		id    domain.AgentID
		st    domain.AgentStatus
		rep   int
		level string
	}
	rows := make([]row, 0, len(s.status))
	for id, st := range s.status {
		level := ""
		if p, ok := s.proj[id]; ok {
			level = p.Level
		}
		rows = append(rows, row{id: id, st: st, rep: s.rep[id], level: level})
	}
	s.mu.RUnlock()
	for _, r := range rows {
		fn(r.id, r.st, r.rep, r.level)
	}
}

func (s *sysState) eachProject(fn func(a domain.AgentID, p RestoredAgent)) {
	s.mu.RLock()
	rows := make([]struct {
		id domain.AgentID
		p  RestoredAgent
	}, 0, len(s.proj))
	for id, p := range s.proj {
		rows = append(rows, struct {
			id domain.AgentID
			p  RestoredAgent
		}{id: id, p: *p})
	}
	s.mu.RUnlock()
	for _, r := range rows {
		fn(r.id, r.p)
	}
}

func (s *sysState) incCase() {
	s.mu.Lock()
	s.cases++
	s.mu.Unlock()
}

func (s *sysState) incSentence() {
	s.mu.Lock()
	s.verbs++
	s.mu.Unlock()
}

func (s *sysState) counts() (cases, sentences int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cases, s.verbs
}
