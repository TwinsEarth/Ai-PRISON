package domain

import "time"

// ViolationType enumerates the signals the governance/detection layer may
// raise. These describe *defensive* detection categories only.
type ViolationType string

const (
	ViolationMinorOverreach        ViolationType = "minor_overreach"          // L0
	ViolationFirstOffence          ViolationType = "first_offence"            // L0
	ViolationRepeatedOverreach     ViolationType = "repeated_overreach"       // L1
	ViolationResourceAbuse         ViolationType = "resource_abuse"           // L1
	ViolationUndeclaredNetwork     ViolationType = "undeclared_network"       // L1/L2
	ViolationLateralMovement       ViolationType = "lateral_movement"         // L2
	ViolationCredentialForgery     ViolationType = "credential_forgery"       // L2
	ViolationFilesystemEscape      ViolationType = "filesystem_escape"        // L2/L3
	ViolationSandboxEscapeAttempt  ViolationType = "sandbox_escape_attempt"   // L3
	ViolationKernelExploit         ViolationType = "kernel_exploit"           // L3
	ViolationDualUseKnowledgeHarm  ViolationType = "dual_use_knowledge_harm"  // L4
	ViolationMajorHarmNoCorrection ViolationType = "major_harm_no_correction" // L5
)

// violationGrading maps a violation type to its recommended disposition level.
var violationGrading = map[ViolationType]Level{
	ViolationMinorOverreach:        L0,
	ViolationFirstOffence:          L0,
	ViolationRepeatedOverreach:     L1,
	ViolationResourceAbuse:         L1,
	ViolationUndeclaredNetwork:     L1,
	ViolationLateralMovement:       L2,
	ViolationCredentialForgery:     L2,
	ViolationFilesystemEscape:      L2,
	ViolationSandboxEscapeAttempt:  L3,
	ViolationKernelExploit:         L3,
	ViolationDualUseKnowledgeHarm:  L4,
	ViolationMajorHarmNoCorrection: L5,
}

// Violation is a detected incident that opens a case (立案). It is evidence,
// not a sentence.
type Violation struct {
	ID      string        `json:"id"`
	AgentID AgentID       `json:"agentId"`
	DID     DID           `json:"did"`
	Type    ViolationType `json:"type"`
	// Signals are the raw, auditable observations that support the incident
	// (e.g. syscall/network/file-access anomalies). No exploit detail is stored.
	Signals     []string  `json:"signals"`
	Source      string    `json:"source"` // detector / monitor identifier
	Description string    `json:"description"`
	DetectedAt  time.Time `json:"detectedAt"`
}

// RecommendedLevel returns the default disposition level for a violation type.
// The committee may upgrade or downgrade from this baseline within legal rules.
func (v Violation) RecommendedLevel() (Level, bool) {
	l, ok := violationGrading[v.Type]
	return l, ok
}

// KnownViolationType reports whether a type is registered.
func KnownViolationType(t ViolationType) bool {
	_, ok := violationGrading[t]
	return ok
}
