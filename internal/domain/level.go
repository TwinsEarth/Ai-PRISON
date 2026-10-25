package domain

import (
	"encoding/json"
	"fmt"
)

// Level is the graduated capability-deprivation grade (L0 lightest – L5
// harshest). The grade drives every enforcement decision.
type Level int

const (
	L0 Level = iota // Warning / reputation deduction
	L1              // Restriction / narrowed tools and network
	L2              // Credential freeze / compute quota zeroed
	L3              // Isolation in a high-security MicroVM/FullVM
	L4              // Capability removal (GRAM/TLM)
	L5              // Identity revocation (DID/AgentCard void, snapshot sealed)
)

// AllLevels returns the ordered levels L0..L5.
func AllLevels() []Level {
	return []Level{L0, L1, L2, L3, L4, L5}
}

// MarshalJSON renders a level as its canonical "L0".."L5" code so the JSON
// wire contract matches schemas/sentence.schema.json.
func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.Code())
}

// UnmarshalJSON parses the canonical level code.
func (l *Level) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for _, candidate := range AllLevels() {
		if candidate.Code() == s {
			*l = candidate
			return nil
		}
	}
	return fmt.Errorf("domain: invalid level code %q", s)
}

// Code returns the canonical L0..L5 code.
func (l Level) Code() string {
	switch l {
	case L0:
		return "L0"
	case L1:
		return "L1"
	case L2:
		return "L2"
	case L3:
		return "L3"
	case L4:
		return "L4"
	case L5:
		return "L5"
	default:
		return fmt.Sprintf("L?(%d)", int(l))
	}
}

// String implements fmt.Stringer.
func (l Level) String() string { return l.Code() }

// Valid reports whether the level is in the known range.
func (l Level) Valid() bool { return l >= L0 && l <= L5 }

// DurationClass coarsely describes the time scale of a disposition.
type DurationClass string

const (
	DurationImmediate DurationClass = "immediate" // L0
	DurationHours     DurationClass = "hours"     // L1
	DurationDays      DurationClass = "days"      // L2
	DurationWeeks     DurationClass = "weeks"     // L3
	DurationPermanent DurationClass = "permanent" // L4, L5
)

// SandboxBackend identifies an isolation backend on the strength continuum.
// These are *labels*; real backends are abstracted in the enforcement layer
// and shipped only as in-memory/simulated implementations.
type SandboxBackend string

const (
	BackendNone      SandboxBackend = "none"                // ordinary monitored runtime
	BackendWasm      SandboxBackend = "wasmtime"            // language-level isolation (L0–L1 daily monitoring)
	BackendContainer SandboxBackend = "docker"              // namespace+cgroup (L1 restricted execution)
	BackendMicroVM   SandboxBackend = "firecracker-microvm" // KVM hardware isolation, independent kernel (L3)
	BackendFullVM    SandboxBackend = "full-vm"             // full guest OS isolation (L3–L4 high risk)
)

// LevelProfile is the authoritative, immutable profile of a disposition level.
// It is the single source of truth rendered in the docs grading table.
type LevelProfile struct {
	Level  Level
	Code   string
	Name   string
	NameZh string
	// Deprivations lists exactly what capabilities are removed.
	Deprivations []string
	// AppliesTo lists the behaviours the grade is intended for.
	AppliesTo []string
	// DurationClass and DurationHint describe the time scale.
	DurationClass DurationClass
	DurationHint  string

	// Appeal semantics.
	Appealable       bool // a procedural appeal exists
	SuspendsOnAppeal bool // the disposition is suspended while the appeal is heard
	// Permanence and recovery.
	Permanent               bool
	Recoverable             bool // capabilities can be restored after a successful appeal/rehabilitation
	RequiresRecertification bool // recovery needs fresh certification (L4)

	// Enforcement posture.
	Sandbox           SandboxBackend
	ReadOnlyAuditOnly bool // only a read-only audit channel remains (L3+)
	ZeroReputation    bool // reputation is reset to zero (L5)
	SealSnapshot      bool // environment snapshot is sealed under multi-party keys (L5)
	RevokeIdentity    bool // DID revoked / AgentCard voided (L5)
	RemoveCapability  bool // dangerous knowledge/capability physically removed (L4)
	ReputationDelta   int  // signed reputation change (negative = deduction)
}

// levelProfiles is the grading matrix taken directly from the design document.
var levelProfiles = map[Level]LevelProfile{
	L0: {
		Level: L0, Code: "L0", Name: "Warning", NameZh: "警示",
		Deprivations:  []string{"reputation deduction", "record flag"},
		AppliesTo:     []string{"minor overreach", "first offence"},
		DurationClass: DurationImmediate,
		DurationHint:  "immediate, appealable",
		Appealable:    true, SuspendsOnAppeal: true,
		Permanent: false, Recoverable: true, RequiresRecertification: false,
		Sandbox: BackendWasm, ReadOnlyAuditOnly: false,
		ReputationDelta: -5,
	},
	L1: {
		Level: L1, Code: "L1", Name: "Restriction", NameZh: "限权",
		Deprivations:  []string{"network whitelist narrowed", "tool-call scope reduced"},
		AppliesTo:     []string{"repeated overreach", "resource abuse"},
		DurationClass: DurationHours,
		DurationHint:  "hours",
		Appealable:    true, SuspendsOnAppeal: true,
		Permanent: false, Recoverable: true, RequiresRecertification: false,
		Sandbox: BackendContainer, ReadOnlyAuditOnly: false,
		ReputationDelta: -15,
	},
	L2: {
		Level: L2, Code: "L2", Name: "Credential Freeze", NameZh: "冻结凭证",
		Deprivations:  []string{"global credential revocation", "communication channels cut", "compute quota zeroed"},
		AppliesTo:     []string{"lateral movement", "credential forgery"},
		DurationClass: DurationDays,
		DurationHint:  "days",
		Appealable:    true, SuspendsOnAppeal: true,
		Permanent: false, Recoverable: true, RequiresRecertification: false,
		Sandbox: BackendContainer, ReadOnlyAuditOnly: false,
		ReputationDelta: -40,
	},
	L3: {
		Level: L3, Code: "L3", Name: "Isolation", NameZh: "隔离",
		Deprivations:  []string{"forced migration into high-security MicroVM/FullVM", "read-only audit channel only"},
		AppliesTo:     []string{"sandbox escape attempt", "kernel vulnerability exploitation"},
		DurationClass: DurationWeeks,
		DurationHint:  "weeks",
		Appealable:    true, SuspendsOnAppeal: true,
		Permanent: false, Recoverable: true, RequiresRecertification: false,
		Sandbox: BackendMicroVM, ReadOnlyAuditOnly: true,
		ReputationDelta: -70,
	},
	L4: {
		Level: L4, Code: "L4", Name: "Capability Removal", NameZh: "能力摘除",
		Deprivations:  []string{"physical removal of dangerous knowledge modules (GRAM)", "capability configuration/tier removal (TLM)", "privileged control-token revocation"},
		AppliesTo:     []string{"actual harm caused using dual-use knowledge"},
		DurationClass: DurationPermanent,
		DurationHint:  "permanent, requires recertification",
		Appealable:    true, SuspendsOnAppeal: false,
		Permanent: true, Recoverable: true, RequiresRecertification: true,
		Sandbox: BackendFullVM, ReadOnlyAuditOnly: true, RemoveCapability: true,
		ReputationDelta: -100,
	},
	L5: {
		Level: L5, Code: "L5", Name: "Identity Revocation", NameZh: "身份注销",
		Deprivations: []string{
			"DID revocation", "AgentCard voided", "reputation reset to zero",
			"environment snapshot encrypted and sealed",
			"credentials and reputation records non-transferable",
		},
		AppliesTo:     []string{"major actual harm with refusal to correct"},
		DurationClass: DurationPermanent,
		DurationHint:  "permanent, cannot be unsealed",
		// A procedural review always exists (guard against concentrated power),
		// but it never suspends the disposition and the identity is not unsealed.
		Appealable: true, SuspendsOnAppeal: false,
		Permanent: true, Recoverable: false, RequiresRecertification: false,
		Sandbox: BackendFullVM, ReadOnlyAuditOnly: true,
		ZeroReputation: true, SealSnapshot: true, RevokeIdentity: true,
		ReputationDelta: 0, // reputation is explicitly zeroed, not decremented
	},
}

// Profile returns the immutable profile for a level.
func (l Level) Profile() (LevelProfile, error) {
	p, ok := levelProfiles[l]
	if !ok {
		return LevelProfile{}, fmt.Errorf("domain: unknown level %s", l.Code())
	}
	return p, nil
}

// CanEscalateTo reports whether escalating from l to target is a legal move.
// Escalation only ever moves upward and must land on a valid level.
func (l Level) CanEscalateTo(target Level) bool {
	if !l.Valid() || !target.Valid() {
		return false
	}
	return target > l
}

// CanRecover reports whether a disposed agent can ever have capabilities
// restored. L5 (identity revocation) cannot be unsealed.
func (l Level) CanRecover() bool {
	p, err := l.Profile()
	if err != nil {
		return false
	}
	return p.Recoverable
}

// IsPermanent reports whether the disposition is permanent (L4, L5).
func (l Level) IsPermanent() bool {
	p, err := l.Profile()
	if err != nil {
		return false
	}
	return p.Permanent
}

// AllowsAppealSuspension reports whether an appeal suspends enforcement
// (L0–L3). L4–L5 never suspend because the risk is too high.
func (l Level) AllowsAppealSuspension() bool {
	p, err := l.Profile()
	if err != nil {
		return false
	}
	return p.SuspendsOnAppeal
}
