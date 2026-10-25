package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// AccountabilityInput carries the two judgements the human-side ruling needs:
// whether the violation happened inside the scope the human principal
// authorised, and whether it exploited the model's inherent dangerous
// (dual-use) knowledge.
type AccountabilityInput struct {
	// WithinAuthorization is true when the violation occurred inside the scope
	// the human principal authorised.
	WithinAuthorization bool
	// DualUseKnowledge is true when the violation exploited the model's
	// inherent dangerous knowledge (dual-use knowledge the provider shipped).
	DualUseKnowledge bool
	// Reason is recorded on every emitted finding.
	Reason string
	// Now overrides the recorded-at timestamp; zero means time.Now().UTC().
	Now time.Time
}

// DetermineAccountability maps the two judgements onto the three human-side
// roles exactly as the design mandates:
//
//   - within authorisation  -> the human PRINCIPAL bears joint liability
//     (FindingJointLiability);
//   - outside authorisation -> the DEPLOYER's monitoring duty is reviewed
//     (FindingMonitoringDutyReview);
//   - dual-use knowledge was exploited -> the MODEL PROVIDER's capability-control
//     duty is additionally reviewed (FindingCapabilityControlDutyReview).
//
// The principal-vs-deployer base finding is mutually exclusive; the
// provider finding is additive (it is recorded in addition, never instead).
// The returned slice is always non-empty (at least the base finding), which is
// the precondition the RevokeIdentity invariant gate relies on.
func DetermineAccountability(a *domain.Agent, in AccountabilityInput) []domain.AccountabilityRecord {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	recs := make([]domain.AccountabilityRecord, 0, 2)
	if in.WithinAuthorization {
		recs = append(recs, domain.AccountabilityRecord{
			Party:               a.Principal,
			Role:                domain.RolePrincipal,
			Finding:             domain.FindingJointLiability,
			WithinAuthorization: true,
			Reason:              in.Reason,
			RecordedAt:          now,
		})
	} else {
		recs = append(recs, domain.AccountabilityRecord{
			Party:               a.Deployer,
			Role:                domain.RoleDeployer,
			Finding:             domain.FindingMonitoringDutyReview,
			WithinAuthorization: false,
			Reason:              in.Reason,
			RecordedAt:          now,
		})
	}
	if in.DualUseKnowledge {
		recs = append(recs, domain.AccountabilityRecord{
			Party:               a.ModelProviderParty,
			Role:                domain.RoleModelProvider,
			Finding:             domain.FindingCapabilityControlDutyReview,
			WithinAuthorization: in.WithinAuthorization,
			Reason:              "dual-use knowledge exploited: " + in.Reason,
			RecordedAt:          now,
		})
	}
	return recs
}

// ValidateAccountability enforces the mandatory invariant: every identity
// revocation MUST carry at least one well-formed human accountability record.
// It returns an error — which the orchestrator surfaces as a failed
// disposition — when records is empty or any record lacks a party, a known
// role, a concrete finding, or a recorded-at timestamp.
func ValidateAccountability(records []domain.AccountabilityRecord) error {
	if len(records) == 0 {
		return errors.New("identity: disposition rejected — every identity revocation must carry at least one human accountability record (human accountability coupling, principle 3)")
	}
	for i, r := range records {
		if r.Party.ID == "" {
			return fmt.Errorf("identity: accountability record %d has no human party id", i)
		}
		switch r.Role {
		case domain.RolePrincipal, domain.RoleDeployer, domain.RoleModelProvider:
		default:
			return fmt.Errorf("identity: accountability record %d has unknown role %q", i, r.Role)
		}
		if r.Finding == "" || r.Finding == domain.FindingNone {
			return fmt.Errorf("identity: accountability record %d has no concrete finding", i)
		}
		if r.RecordedAt.IsZero() {
			return fmt.Errorf("identity: accountability record %d has no recordedAt", i)
		}
	}
	return nil
}
