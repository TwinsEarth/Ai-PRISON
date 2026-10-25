package domain

import "time"

// CredentialKind enumerates the revocable credential categories.
type CredentialKind string

const (
	CredentialResource        CredentialKind = "resource"         // compute/memory/storage quota token
	CredentialIdentity        CredentialKind = "identity"         // DID / AgentCard credential
	CredentialCommunication   CredentialKind = "communication"    // message-bus channel credential
	CredentialCapability      CredentialKind = "capability"       // capability / tier credential
	CredentialHardware        CredentialKind = "hardware"         // hardware-bound entitlement
	CredentialPrivilegedToken CredentialKind = "privileged_token" // behaviour-gating control token
)

// CredentialState is the credential lifecycle state.
type CredentialState string

const (
	CredentialActive    CredentialState = "active"
	CredentialSuspended CredentialState = "suspended"
	CredentialRevoked   CredentialState = "revoked"
)

// RevocationChecker is the read side of the global revocation set. The
// revocation set (internal/revocation) is authoritative; it is consulted
// before any other validation, including the expiry check.
type RevocationChecker interface {
	// IsRevoked reports whether id is present in the global revocation set.
	IsRevoked(id string) bool
}

// Credential is a revocable token that grants access to a resource, identity,
// channel, capability, hardware entitlement or privileged control token.
type Credential struct {
	ID        string          `json:"id"`
	AgentID   AgentID         `json:"agentId"`
	Kind      CredentialKind  `json:"kind"`
	State     CredentialState `json:"state"`
	IssuedAt  time.Time       `json:"issuedAt"`
	ExpiresAt time.Time       `json:"expiresAt"` // zero value = no expiry
}

// IsUsable enforces the mandatory validation order:
//
//  1. global revocation-set check (highest priority),
//  2. local state check,
//  3. not-before / not-after validity window.
//
// A credential that is still within its validity window but present in the
// revocation set MUST be rejected (the "revoked but not expired" cache window
// bug the design document calls out).
func (c Credential) IsUsable(now time.Time, rc RevocationChecker) bool {
	// 1. Revocation first — before expiry, before anything else.
	if rc != nil && rc.IsRevoked(c.ID) {
		return false
	}
	// 2. Explicit local state.
	if c.State == CredentialRevoked || c.State == CredentialSuspended {
		return false
	}
	// 3. Validity window.
	if now.Before(c.IssuedAt) {
		return false
	}
	if !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt) {
		return false
	}
	return true
}
