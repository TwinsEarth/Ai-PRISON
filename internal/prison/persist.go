package prison

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/enforcement/resource"
	"github.com/ai-prison/ai-prison/internal/revocation"
	"github.com/ai-prison/ai-prison/internal/store"
)

// This file wires the durable, append-only governance ledger (internal/store)
// into the orchestration System. It is the v1.1 "real persistence" layer:
//
//   - WHEN Config.Store == nil (the default), EVERYTHING below is compiled out
//     at runtime: the System behaves byte-for-byte like v1.0. No ledger record
//     is written, no replay happens and no output changes.
//   - WHEN a Store is supplied, the same in-memory substrates are built first,
//     then the ledger is replayed to rebuild them (event sourcing), and every
//     live state change is projected onto the ledger at well-defined journey
//     boundaries.
//
// # BOUNDARY — what is and is NOT persisted
//
// Persisted (real, crash-safe on disk): the judgement hash chain, revocation
// set entries + node acks, execution receipts, sealed-snapshot metadata, agent
// registrations, adjudicated cases, issued sentences and the orchestrator's
// runtime status projection. These are the GOVERNANCE / AUDIT RECORD.
//
// NOT persisted and still fully simulated: the execution backends themselves —
// sandbox isolation, cgroup/quotas, real revocation distribution, hardware
// binding, GRAM/TLM and token stacks. Restarting the process does NOT resume a
// running sandbox; it rebuilds the governance ledger so the next process can
// prove what was decided and enforce it again. This package is strictly
// defensive: traceability, verifiability, accountable recovery — no escape,
// attack or weaponization.

// recordingSink adapts *audit.Chain to the structural EventSink shape used by
// every enforcement/governance layer. When a ledger is wired it (a) stamps a
// deterministic timestamp on events that carry none, so demo output is stable,
// and (b) mirrors every stamped event onto the ledger as KindAuditEvent. It is
// ONLY constructed when Config.Store != nil.
type recordingSink struct {
	chain *audit.Chain
	rec   *ledgerRecorder
	clock func() time.Time
}

// Append satisfies resource/identity/migration/capability/governance/isolation
// EventSink interfaces structurally.
func (s recordingSink) Append(ev domain.Event) domain.Event {
	if ev.Timestamp.IsZero() && s.clock != nil {
		ev.Timestamp = s.clock().UTC()
	}
	out := s.chain.Append(ev)
	if s.rec != nil {
		s.rec.appendAuditEvent(out)
	}
	return out
}

// --- ledger record payload envelopes (JSON on the wire; version 1) ----------

// revEntryRec is KindRevocationEntry: one entry plus its write-time holding
// nodes, so the entry can be replayed onto a fresh set.
type revEntryRec struct {
	revocation.Entry
	HoldingNodes []string `json:"holdingNodes,omitempty"`
}

// revAckRec is KindRevocationAck: one node's acknowledgement of a credential.
type revAckRec struct {
	CredentialID string `json:"credentialId"`
	NodeID       string `json:"nodeId"`
}

// agentRegRec is KindAgent: the immutable registration facts of one agent.
type agentRegRec struct {
	AgentID   domain.AgentID    `json:"agentId"`
	DID       domain.DID        `json:"did"`
	Name      string            `json:"name"`
	Principal domain.HumanParty `json:"principal"`
	Nodes     []string          `json:"nodes"`
}

// runtimeStatusRec is KindRuntimeStatus: the orchestrator's read-side status
// projection for one agent. Later records for the same agent win on replay.
type runtimeStatusRec struct {
	AgentID    domain.AgentID     `json:"agentId"`
	Status     domain.AgentStatus `json:"status"`
	Level      string             `json:"level"`
	Reputation int                `json:"reputation"`
}

// RestoredAgent is the read-side projection of one agent, rebuilt from the
// ledger on restart. It never drives behaviour; it exists so `inspect` can
// report exactly what the durable record says happened.
type RestoredAgent struct {
	AgentID     domain.AgentID
	DID         domain.DID
	Name        string
	Principal   string
	Nodes       []string
	CardRevoked bool
	Reputation  int
	Level       string
	Status      domain.AgentStatus
	Sentences   []string
}

// ledgerRecorder owns the dedup state that makes live projection idempotent:
// an already-recorded id is written once (receipts/status only when they
// actually change). It is nil when Config.Store == nil.
type ledgerRecorder struct {
	st store.Store

	seenEntry    map[string]bool
	seenAck      map[string]bool
	receiptSig   map[string]string // receiptID -> last persisted "status|sig|ackedAt"
	seenSnap     map[string]bool
	seenAgent    map[domain.AgentID]bool
	seenSentence map[string]bool
	seenCase     map[string]bool
	statusSig    map[domain.AgentID]string // agent -> last persisted "status|level|rep"

	// sticky error: the first Append failure survives to Close.
	err error
}

func newRecorder(st store.Store) *ledgerRecorder {
	return &ledgerRecorder{
		st:           st,
		seenEntry:    map[string]bool{},
		seenAck:      map[string]bool{},
		receiptSig:   map[string]string{},
		seenSnap:     map[string]bool{},
		seenAgent:    map[domain.AgentID]bool{},
		seenSentence: map[string]bool{},
		seenCase:     map[string]bool{},
		statusSig:    map[domain.AgentID]string{},
	}
}

// append records one payload; the first failure sticks and later appends are
// skipped so a broken ledger does not go silently on appending new rows.
func (r *ledgerRecorder) append(kind string, payload any) {
	if r == nil || r.err != nil {
		return
	}
	if _, err := r.st.Append(context.Background(), kind, payload); err != nil {
		r.err = err
	}
}

// appendAuditEvent mirrors one stamped chain event onto the ledger.
func (r *ledgerRecorder) appendAuditEvent(ev domain.Event) {
	r.append(store.KindAuditEvent, ev)
}

// checkpoint projects every live substrate onto the ledger, writing only what
// is new or changed since the previous checkpoint. Called at the clear journey
// stage boundaries (see pipeline.go).
func (sys *System) checkpoint() {
	if sys.recorder == nil || sys.recorder.err != nil {
		return
	}
	r := sys.recorder

	// 1. revocation entries + their newly-acked nodes.
	for _, d := range sys.Set.ListDetailed() {
		if !r.seenEntry[d.ID] {
			r.append(store.KindRevocationEntry, revEntryRec{Entry: d.Entry, HoldingNodes: d.RequiredNodes})
			r.seenEntry[d.ID] = true
		}
		for _, n := range d.AcknowledgedBy {
			key := d.ID + "|" + n
			if !r.seenAck[key] {
				r.append(store.KindRevocationAck, revAckRec{CredentialID: d.ID, NodeID: n})
				r.seenAck[key] = true
			}
		}
	}

	// 2. receipts — re-written only when their state/signature changed.
	for _, rc := range sys.Book.All() {
		acked := ""
		if rc.AckedAt != nil {
			acked = rc.AckedAt.UTC().Format(time.RFC3339Nano)
		}
		sig := string(rc.Status) + "|" + rc.Signature + "|" + acked
		if r.receiptSig[rc.ID] != sig {
			r.append(store.KindReceipt, rc)
			r.receiptSig[rc.ID] = sig
		}
	}

	// 3. sealed-snapshot metadata (immutable, once per snapshot id).
	for _, sn := range sys.ResSnaps.All() {
		if !r.seenSnap[sn.ID] {
			r.append(store.KindSnapshot, sn)
			r.seenSnap[sn.ID] = true
		}
	}

	// 4. new cases and sentences.
	for _, c := range sys.Gov.ListCases() {
		if !r.seenCase[c.ID] {
			r.append(store.KindCase, c)
			r.seenCase[c.ID] = true
			sys.caseCount++
		}
	}
	for _, s := range sys.Gov.ListSentences() {
		if !r.seenSentence[s.ID] {
			r.append(store.KindSentence, s)
			r.seenSentence[s.ID] = true
			sys.sentenceCount++
		}
	}

	// 5. runtime status projection (last write wins on replay).
	for id, st := range sys.runtimeStatus {
		rep := sys.agentReputation[id]
		level := ""
		if p, ok := sys.proj[id]; ok {
			level = p.Level
		}
		sig := string(st) + "|" + level + "|" + fmt.Sprintf("%d", rep)
		if r.statusSig[id] != sig {
			r.append(store.KindRuntimeStatus, runtimeStatusRec{AgentID: id, Status: st, Level: level, Reputation: rep})
			r.statusSig[id] = sig
		}
	}
}

// recordAgentRegistration projects one agent onto the ledger (called once from
// SeedAgent, when Config.Store != nil).
func (sys *System) recordAgentRegistration(agent domain.Agent, nodes []string) {
	if sys.recorder == nil || sys.recorder.err != nil {
		return
	}
	r := sys.recorder
	if r.seenAgent[agent.ID] {
		return
	}
	cp := append([]string(nil), nodes...)
	sort.Strings(cp)
	rec := agentRegRec{
		AgentID:   agent.ID,
		DID:       agent.Card.DID,
		Name:      agent.Card.Name,
		Principal: agent.Principal,
		Nodes:     cp,
	}
	r.append(store.KindAgent, rec)
	r.seenAgent[agent.ID] = true
}

// --- event-sourced replay ----------------------------------------------------

// replay rebuilds every substrate and the Restored projection from the ledger.
// The ledger itself was already integrity-verified when the FileStore was
// opened; a payload decode failure here is a programming error and sticks on
// sys.BootstrapErr.
func (sys *System) replay() error {
	if sys.recorder == nil {
		return nil
	}
	r := sys.recorder
	return r.st.Replay(context.Background(), func(rec store.Record) error {
		switch rec.Kind {
		case store.KindAuditEvent:
			var ev domain.Event
			if err := json.Unmarshal(rec.Payload, &ev); err != nil {
				return fmt.Errorf("prison: replay audit event seq=%d: %w", rec.Seq, err)
			}
			// Re-feeding in order re-derives identical index/prevHash/hash.
			sys.Chain.Append(ev)

		case store.KindRevocationEntry:
			var env revEntryRec
			if err := json.Unmarshal(rec.Payload, &env); err != nil {
				return fmt.Errorf("prison: revocation entry seq=%d: %w", rec.Seq, err)
			}
			sys.Set.Revoke(env.Entry, env.HoldingNodes)
			r.seenEntry[env.Entry.ID] = true
			for _, n := range env.Entry.AcknowledgedBy {
				r.seenAck[env.Entry.ID+"|"+n] = true
			}
			if env.Entry.Kind == domain.CredentialIdentity {
				if p, ok := sys.proj[env.Entry.AgentID]; ok {
					p.CardRevoked = true
				}
			}

		case store.KindRevocationAck:
			var a revAckRec
			if err := json.Unmarshal(rec.Payload, &a); err != nil {
				return fmt.Errorf("prison: revocation ack seq=%d: %w", rec.Seq, err)
			}
			sys.Set.AckNode(a.CredentialID, a.NodeID)
			r.seenAck[a.CredentialID+"|"+a.NodeID] = true

		case store.KindReceipt:
			var rc domain.Receipt
			if err := json.Unmarshal(rec.Payload, &rc); err != nil {
				return fmt.Errorf("prison: receipt seq=%d: %w", rec.Seq, err)
			}
			sys.Book.Restore(rc)
			acked := ""
			if rc.AckedAt != nil {
				acked = rc.AckedAt.UTC().Format(time.RFC3339Nano)
			}
			r.receiptSig[rc.ID] = string(rc.Status) + "|" + rc.Signature + "|" + acked

		case store.KindSnapshot:
			var sn resource.Snapshot
			if err := json.Unmarshal(rec.Payload, &sn); err != nil {
				return fmt.Errorf("prison: snapshot seq=%d: %w", rec.Seq, err)
			}
			if _, err := sys.ResSnaps.Restore(sn); err != nil {
				return err
			}
			r.seenSnap[sn.ID] = true

		case store.KindAgent:
			var ar agentRegRec
			if err := json.Unmarshal(rec.Payload, &ar); err != nil {
				return fmt.Errorf("prison: agent reg seq=%d: %w", rec.Seq, err)
			}
			sys.proj[ar.AgentID] = &RestoredAgent{
				AgentID:   ar.AgentID,
				DID:       ar.DID,
				Name:      ar.Name,
				Principal: ar.Principal.ID,
				Nodes:     ar.Nodes,
			}
			r.seenAgent[ar.AgentID] = true

		case store.KindSentence:
			var s domain.Sentence
			if err := json.Unmarshal(rec.Payload, &s); err != nil {
				return fmt.Errorf("prison: sentence seq=%d: %w", rec.Seq, err)
			}
			if !r.seenSentence[s.ID] {
				sys.sentenceCount++
				p, ok := sys.proj[s.AgentID]
				if !ok {
					p = &RestoredAgent{AgentID: s.AgentID}
					sys.proj[s.AgentID] = p
				}
				p.Sentences = append(p.Sentences, s.ID)
			}
			r.seenSentence[s.ID] = true

		case store.KindCase:
			var c struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Payload, &c); err != nil {
				return fmt.Errorf("prison: case seq=%d: %w", rec.Seq, err)
			}
			if !r.seenCase[c.ID] {
				sys.caseCount++
			}
			r.seenCase[c.ID] = true

		case store.KindRuntimeStatus:
			var rs runtimeStatusRec
			if err := json.Unmarshal(rec.Payload, &rs); err != nil {
				return fmt.Errorf("prison: runtime status seq=%d: %w", rec.Seq, err)
			}
			p, ok := sys.proj[rs.AgentID]
			if !ok {
				p = &RestoredAgent{AgentID: rs.AgentID}
				sys.proj[rs.AgentID] = p
			}
			p.Status = rs.Status
			p.Level = rs.Level
			p.Reputation = rs.Reputation
			r.statusSig[rs.AgentID] = string(rs.Status) + "|" + rs.Level + "|" + fmt.Sprintf("%d", rs.Reputation)
		}
		return nil
	})
}

// --- structured inspection view ----------------------------------------------

// SnapshotView is one sealed-snapshot row in the inspect view.
type SnapshotView struct {
	ID          string `json:"id"`
	AgentID     string `json:"agentId"`
	ContentHash string `json:"contentHash"`
}

// AgentView is one row of the per-agent inspect projection.
type AgentView struct {
	AgentID     string `json:"agentId"`
	DID         string `json:"did"`
	Level       string `json:"level"`
	Status      string `json:"status"`
	CardRevoked bool   `json:"cardRevoked"`
	Reputation  int    `json:"reputation"`
}

// InspectView is the structured, backend-independent snapshot of a System: it
// is identical in shape whether the System is purely in-memory or rebuilt from
// a durable ledger, so the CLI inspect command and the e2e tests assert on the
// same facts.
type InspectView struct {
	DataDir string `json:"dataDir,omitempty"`
	// Restored is true when this view was rebuilt by replaying a ledger.
	Restored bool `json:"restored"`

	StoreRecords    int    `json:"storeRecords"`
	ChainEvents     int    `json:"chainEvents"`
	ChainVerifyOK   bool   `json:"chainVerifyOk"`
	ChainVerifyNote string `json:"chainVerifyNote,omitempty"`

	RevocationEntries  int      `json:"revocationEntries"`
	RevocationEnforced int      `json:"revocationEnforced"`
	UntrustedNodes     []string `json:"untrustedNodes,omitempty"`
	ReceiptsAcked      int      `json:"receiptsAcked"`
	ReceiptsOpen       int      `json:"receiptsOpen"`

	Snapshots []SnapshotView `json:"snapshots,omitempty"`
	Agents    []AgentView    `json:"agents"`
	Cases     int            `json:"cases"`
	Sentences int            `json:"sentences"`
}

// InspectView builds the structured snapshot described on the type. It reads
// only: it mutates nothing on the System.
func (sys *System) InspectView() *InspectView {
	v := &InspectView{Restored: sys.restored}
	if sys.recorder != nil {
		v.StoreRecords = sys.recorder.st.Len()
		v.DataDir = sys.recorder.st.Path()
	}

	v.ChainEvents = sys.Chain.Len()
	if err := sys.Chain.Verify(); err == nil {
		v.ChainVerifyOK = true
	} else {
		v.ChainVerifyNote = err.Error()
	}

	for _, d := range sys.Set.ListDetailed() {
		v.RevocationEntries++
		if d.Enforced {
			v.RevocationEnforced++
		}
	}
	v.UntrustedNodes = sys.Book.UntrustedNodes()
	for _, rc := range sys.Book.All() {
		if rc.Status == domain.ReceiptAcked {
			v.ReceiptsAcked++
		} else {
			v.ReceiptsOpen++
		}
	}

	for _, sn := range sys.ResSnaps.All() {
		v.Snapshots = append(v.Snapshots, SnapshotView{
			ID:          sn.ID,
			AgentID:     string(sn.AgentID),
			ContentHash: sn.ContentHash,
		})
	}

	// Unify the projection (live: incrementally maintained; restart: replayed).
	for id, p := range sys.proj {
		v.Agents = append(v.Agents, AgentView{
			AgentID:     string(id),
			DID:         string(p.DID),
			Level:       p.Level,
			Status:      string(p.Status),
			CardRevoked: p.CardRevoked,
			Reputation:  p.Reputation,
		})
	}
	sort.Slice(v.Agents, func(i, j int) bool { return v.Agents[i].AgentID < v.Agents[j].AgentID })

	if sys.recorder == nil {
		// Pure in-memory mode: read the live governance book directly.
		v.Cases = len(sys.Gov.ListCases())
		v.Sentences = len(sys.Gov.ListSentences())
	} else {
		v.Cases = sys.caseCount
		v.Sentences = sys.sentenceCount
	}
	return v
}

// Close flushes the ledger (fsync on FileStore) and reports the first sticky
// persistence error. It is a no-op when no ledger is wired.
func (sys *System) Close() error {
	if sys.recorder == nil {
		return nil
	}
	if err := sys.recorder.st.Sync(context.Background()); err != nil && sys.recorder.err == nil {
		sys.recorder.err = err
	}
	_ = sys.recorder.st.Close()
	return sys.recorder.err
}

// SegmentCount counts published segment files in dir (0 when dir is empty or
// missing). Used by the CLI to report "records / segments".
func SegmentCount(dir string) int {
	if dir == "" {
		return 0
	}
	matches, err := filepath.Glob(filepath.Join(dir, "seg-*.log"))
	if err != nil {
		return 0
	}
	return len(matches)
}
