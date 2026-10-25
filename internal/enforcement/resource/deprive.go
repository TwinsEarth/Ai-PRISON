package resource

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ai-prison/ai-prison/internal/audit"
	"github.com/ai-prison/ai-prison/internal/domain"
)

// StepRecord is one auditable row of a Deprive execution, in execution order.
type StepRecord struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// PendingAck is a forced receipt that has been broadcast to a holding node
// but not yet acknowledged. "Broadcast" is never "delivered": this package
// exposes the open acknowledgements and lets the audit receipt book judge
// which nodes are untrusted — it does NOT itself freeze those agents (the
// v0.6 group freeze, 连带冻结).
type PendingAck struct {
	ReceiptID string `json:"receiptId"`
	NodeID    string `json:"nodeId"`
	Operation string `json:"operation"`
}

// DepriveReport is the result of one deprivation run: every step taken, the
// full CPU rung history (proof that the downgrade was gradual), the
// pre-reclaim snapshot and the acknowledgements still outstanding.
type DepriveReport struct {
	SentenceID  string         `json:"sentenceId"`
	AgentID     domain.AgentID `json:"agentId"`
	Steps       []StepRecord   `json:"steps"`
	CPUHistory  []int          `json:"cpuHistory"`
	Snapshot    Snapshot       `json:"snapshot"`
	PendingAcks []PendingAck   `json:"pendingAcks"`
}

// DepriveInput is everything the five-step orchestration needs.
type DepriveInput struct {
	SentenceID   string
	AgentID      domain.AgentID
	CredentialID string   // the revocable resource credential
	Reason       string   // recorded in the revocation entry
	HoldingNodes []string // nodes that must acknowledge; empty = local-only
	Steps        int      // gradual CPU rungs; <=0 means DefaultSteps
}

// Engine wires the injected dependencies and runs the five-step deprivation
// sequence. Dependencies are small interfaces so tests can substitute fakes;
// the real adapters live in adapters.go.
type Engine struct {
	Backend     Backend
	Revocations RevocationSink
	Events      EventSink
	Receipts    ReceiptSink
	Snapshots   SnapshotStore
	Now         func() time.Time
}

// Config builds an Engine; nil fields fall back to safe no-op sinks.
type Config struct {
	Backend     Backend
	Revocations RevocationSink
	Events      EventSink
	Receipts    ReceiptSink
	Snapshots   SnapshotStore
}

// NewEngine returns an Engine with the given dependencies wired.
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		Backend:     cfg.Backend,
		Revocations: cfg.Revocations,
		Events:      cfg.Events,
		Receipts:    cfg.Receipts,
		Snapshots:   cfg.Snapshots,
		Now:         time.Now,
	}
	if e.Revocations == nil {
		e.Revocations = noopRevocations{}
	}
	if e.Events == nil {
		e.Events = noopEvents{}
	}
	if e.Receipts == nil {
		e.Receipts = noopReceipts{}
	}
	return e
}

func (e *Engine) emit(ev domain.Event) {
	e.Events.Append(ev)
}

// Deprive executes the five-step resource-deprivation sequence in the order
// the design document mandates:
//
//  1. write the resource credential into the global revocation set;
//  2. stop new allocations (Allocate now rejects the credential);
//  3. seal the pack_diff snapshot on the still-live pre-reclaim environment,
//     and anchor its hash on the judgement chain;
//  4. gradually reclaim: CPU walks down rungs, memory pages are frozen, the
//     network is cut — never an instant zero;
//  5. force receipts: broadcast one snapshot-seal receipt per holding node.
//
// The order is observable from the returned report and from the event
// sequence on the chain: EventSnapshotSealed always precedes every
// EventResourceReclaimed, and EventRevocationWritten precedes
// EventAllocationStopped.
func (e *Engine) Deprive(in DepriveInput) (*DepriveReport, error) {
	if in.Steps <= 0 {
		in.Steps = DefaultSteps
	}
	if e.Backend == nil {
		return nil, fmt.Errorf("resource: deprivation needs a Backend")
	}

	rep := &DepriveReport{SentenceID: in.SentenceID, AgentID: in.AgentID}

	// Step 1: write the global revocation set.
	e.Revocations.RevokeResource(in.CredentialID, in.AgentID, in.SentenceID, in.Reason, in.HoldingNodes)
	e.emit(domain.Event{
		Type:       domain.EventRevocationWritten,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "resource-deprivation",
		Payload:    map[string]string{"credentialId": in.CredentialID, "kind": string(domain.CredentialResource)},
	})
	rep.Steps = append(rep.Steps, StepRecord{Step: "revocation.written", OK: true, Detail: in.CredentialID})

	// Step 2: stop new allocations. From here on Allocate rejects.
	e.Backend.StopAllocation(in.AgentID)
	e.emit(domain.Event{
		Type:       domain.EventAllocationStopped,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "resource-deprivation",
		Payload:    map[string]string{"credentialId": in.CredentialID},
	})
	rep.Steps = append(rep.Steps, StepRecord{Step: "allocation.stopped", OK: true})

	// Step 3: snapshot sealing BEFORE reclaim — the evidence must capture the
	// live pre-reclaim environment. (The design numbers sealing as step 5, but
	// physically it happens here.)
	current, _ := e.Backend.Current(in.AgentID)
	sealed, err := e.Snapshots.Seal(Snapshot{
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Quota:      current,
		SealedAt:   e.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("resource: pre-reclaim snapshot seal: %w", err)
	}
	rep.Snapshot = sealed
	e.emit(domain.Event{
		Type:       domain.EventSnapshotSealed,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "resource-deprivation",
		Payload: map[string]string{
			"snapshotId":  sealed.ID,
			"contentHash": sealed.ContentHash,
			"preReclaim":  "true",
			"simulated":   "true",
		},
	})
	rep.Steps = append(rep.Steps, StepRecord{Step: "snapshot.sealed", OK: true, Detail: sealed.ContentHash})

	// Step 4: gradual reclaim.
	if err := e.Backend.PlanReclaim(in.AgentID, in.Steps); err != nil {
		return nil, err
	}
	history := []int{current.CPUMillis}
	for rung := 1; rung <= in.Steps; rung++ {
		after, err := e.Backend.ReclaimStep(in.AgentID)
		if err != nil {
			return nil, err
		}
		history = append(history, after.CPUMillis)
		e.emit(domain.Event{
			Type:       domain.EventResourceReclaimed,
			AgentID:    in.AgentID,
			SentenceID: in.SentenceID,
			Actor:      "resource-deprivation",
			Payload: map[string]string{
				"rung":      strconv.Itoa(rung),
				"ofRungs":   strconv.Itoa(in.Steps),
				"cpuMillis": strconv.Itoa(after.CPUMillis),
				"simulated": "true",
			},
		})
	}
	if err := e.Backend.FreezeMemoryPages(in.AgentID); err != nil {
		return nil, err
	}
	if err := e.Backend.CutNetwork(in.AgentID); err != nil {
		return nil, err
	}
	rep.CPUHistory = history
	rep.Steps = append(rep.Steps, StepRecord{Step: "resource.reclaimed.gradual", OK: true, Detail: fmt.Sprintf("cpu rungs=%d", in.Steps)})

	// Step 5: forced receipts — one snapshot-seal receipt per holding node.
	for _, node := range in.HoldingNodes {
		if node == "" {
			continue
		}
		receipt := domain.Receipt{
			ID:          in.SentenceID + "|" + audit.OpSnapshotSeal + "|" + node,
			Operation:   audit.OpSnapshotSeal,
			SentenceID:  in.SentenceID,
			NodeID:      node,
			BeforeState: currentHash(current),
			AfterState:  sealed.ContentHash,
			IssuedAt:    e.Now().UTC(),
		}
		e.Receipts.RegisterPending(receipt)
		rep.PendingAcks = append(rep.PendingAcks, PendingAck{
			ReceiptID: receipt.ID,
			NodeID:    node,
			Operation: receipt.Operation,
		})
	}
	rep.Steps = append(rep.Steps, StepRecord{Step: "receipts.forced", OK: true, Detail: fmt.Sprintf("%d open acknowledgements", len(rep.PendingAcks))})

	return rep, nil
}

// currentHash renders the pre-reclaim quota as a short before-state handle
// for the forced receipts.
func currentHash(q Quota) string {
	return fmt.Sprintf("cpu=%d;mem=%d;net=%t", q.CPUMillis, q.MemoryBytes, q.NetworkOpen)
}

// --- no-op sinks used when a dependency is intentionally absent -------------

type noopRevocations struct{}

func (noopRevocations) RevokeResource(string, domain.AgentID, string, string, []string) {}

type noopEvents struct{}

func (noopEvents) Append(ev domain.Event) domain.Event { return ev }

type noopReceipts struct{}

func (noopReceipts) RegisterPending(r domain.Receipt) domain.Receipt { return r }
