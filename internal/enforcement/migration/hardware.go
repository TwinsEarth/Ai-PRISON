package migration

import (
	"fmt"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// boundCred is the in-memory row for one hardware-bound credential.
// Simulated, not real hardware binding / root of trust.
type boundCred struct {
	CredID      string
	AgentID     domain.AgentID
	Fingerprint string
	BoundAt     time.Time
}

// MemHardwareBinder is the in-memory HardwareBinder. It consults the REAL
// credential validation order (domain.Credential.IsUsable: revocation set →
// local state → validity window) and then adds the fingerprint check as an
// additional gate. The fingerprint is a plain string — this only deters
// casual copying and is NOT an unbreakable hardware root of trust.
type MemHardwareBinder struct {
	mu     sync.RWMutex
	bounds map[string]boundCred
	now    func() time.Time
}

// NewMemHardwareBinder returns an empty in-memory binder.
func NewMemHardwareBinder() *MemHardwareBinder {
	return &MemHardwareBinder{bounds: make(map[string]boundCred), now: time.Now}
}

// Bind attaches credID to the single fingerprint it may verify on. Binding the
// same credential again to the same fingerprint is an idempotent no-op;
// re-binding to a DIFFERENT fingerprint is an error (a bound credential must
// not silently follow the agent to another machine).
func (b *MemHardwareBinder) Bind(credID string, agentID domain.AgentID, fingerprint string) error {
	if credID == "" {
		return fmt.Errorf("migration: cannot bind an empty credential id")
	}
	if fingerprint == "" {
		return fmt.Errorf("migration: cannot bind %s to an empty hardware fingerprint", credID)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.bounds[credID]; ok {
		if existing.Fingerprint != fingerprint {
			return fmt.Errorf("migration: credential %s already bound to a different fingerprint (re-binding would defeat the binding)", credID)
		}
		return nil
	}
	b.bounds[credID] = boundCred{
		CredID:      credID,
		AgentID:     agentID,
		Fingerprint: fingerprint,
		BoundAt:     b.now().UTC(),
	}
	return nil
}

// Validate enforces, in order:
//
//  1. the real credential order (revocation set → state → validity window), via
//     domain.Credential.IsUsable — a revoked-but-not-expired credential is still
//     rejected;
//  2. IF the credential is hardware-bound, presentFingerprint MUST equal the
//     bound fingerprint — a copy on another machine fails here.
//
// An unbound credential verifies anywhere after step 1.
func (b *MemHardwareBinder) Validate(cred domain.Credential, now time.Time, presentFingerprint string, rc domain.RevocationChecker) (bool, string) {
	// 1. real validation order first.
	if !cred.IsUsable(now, rc) {
		return false, "credential rejected by real order (revocation set / state / validity window)"
	}
	// 2. additional hardware-fingerprint gate.
	b.mu.RLock()
	bound, ok := b.bounds[cred.ID]
	b.mu.RUnlock()
	if !ok {
		return true, "credential not hardware-bound; verifies on any machine"
	}
	if bound.Fingerprint != presentFingerprint {
		// Simulated binding only: this deters casual copying, it is not an
		// unbreakable hardware root of trust.
		return false, fmt.Sprintf("hardware fingerprint mismatch: bound=%q present=%q (SIMULATED binding, not a real root of trust)", bound.Fingerprint, presentFingerprint)
	}
	return true, "credential usable on the bound hardware"
}

// BoundFingerprint reports the fingerprint a credential is bound to.
func (b *MemHardwareBinder) BoundFingerprint(credID string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	bd, ok := b.bounds[credID]
	if !ok {
		return "", false
	}
	return bd.Fingerprint, true
}

// Count returns the number of bound credentials (idempotency tests).
func (b *MemHardwareBinder) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.bounds)
}
