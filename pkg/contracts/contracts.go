// Package contracts defines the stable, public, JSON-schema-aligned wire
// contract for AI Prison. It is the only package outside internal/ that
// external integrators are expected to import. The JSON Schemas in /schemas
// are the authoritative machine-readable form; the types here are aliases
// over the internal domain so there is a single source of truth.
package contracts

import (
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// APIVersion is the wire-contract version. Breaking changes bump this and the
// matching schema $id.
const APIVersion = "v1"

// MediaType is the conventional JSON content type for API payloads.
const MediaType = "application/json"

// Envelope wraps every request/response payload for traceability.
type Envelope struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	RequestID  string    `json:"requestId,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Payload    any       `json:"payload"`
}

// Canonical record aliases (kept in lock-step with /schemas/*.schema.json).
type (
	AgentCard            = domain.AgentCard
	Agent                = domain.Agent
	Violation            = domain.Violation
	ViolationType        = domain.ViolationType
	Sentence             = domain.Sentence
	SentenceStatus       = domain.SentenceStatus
	Charge               = domain.Charge
	Vote                 = domain.Vote
	Credential           = domain.Credential
	CredentialKind       = domain.CredentialKind
	Event                = domain.Event
	EventType            = domain.EventType
	Receipt              = domain.Receipt
	ReceiptStatus        = domain.ReceiptStatus
	AccountabilityRecord = domain.AccountabilityRecord
	Level                = domain.Level
)

// Kinds of envelope payloads.
const (
	KindViolationReport  = "ViolationReport"
	KindSentence         = "Sentence"
	KindReceipt          = "Receipt"
	KindAppeal           = "Appeal"
	KindComplianceReport = "ComplianceReport"
)

// ReportViolationRequest is the input to the detection/case-opening step.
type ReportViolationRequest struct {
	AgentID     domain.AgentID       `json:"agentId"`
	DID         domain.DID           `json:"did"`
	Type        domain.ViolationType `json:"type"`
	Signals     []string             `json:"signals"`
	Source      string               `json:"source"`
	Description string               `json:"description"`
}

// FileAppealRequest is the input to the appeal step; only a human principal
// may file an appeal on the agent's behalf.
type FileAppealRequest struct {
	SentenceID string `json:"sentenceId"`
	FiledBy    string `json:"filedBy"` // human principal id
	Grounds    string `json:"grounds"`
}

// ComplianceReport contrasts per-hop and end-to-end compliance.
type ComplianceReport struct {
	SentenceID        string `json:"sentenceId"`
	PerHopCompliant   bool   `json:"perHopCompliant"`
	EndToEndCompliant bool   `json:"endToEndCompliant"`
	PendingReceipts   int    `json:"pendingReceipts"`
	UntrustedNodes    int    `json:"untrustedNodes"`
	Notes             string `json:"notes,omitempty"`
}
