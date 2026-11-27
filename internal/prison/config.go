package prison

import (
	"fmt"
	"strings"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/store"
)

// This file makes the governance parameters that used to be hard-coded into the
// orchestrator — quotas, custody threshold, freeze threshold and the committee
// roster — first-class configuration. Every default equals the value the
// previous release compiled in, so a zero Config reproduces the 1.9.3 ledger
// byte for byte.
//
// # SAFETY BOUNDS
//
// The committee's four charter seats (agent_safety_org / principal /
// deployer_compliance / model_provider_security) are mandatory by default in
// governance.checkMandatorySeats. `Config.CharterSeats` is an explicit charter
// amendment: a strict subset of those four (at least two seats) that governance
// then treats as mandatory instead. Removing a charter seat requires both the
// narrower list and a non-empty CharterReason, so a roster cannot silently lose
// its safety party; custodyThreshold stays 2..3 (M-of-3; 1 would be
// single-party custody) and the freeze threshold stays 1..1000 (0 would freeze
// every agent on its first signal). Validation rejects values outside those
// bounds instead of silently clamping them.

// QuotaSpec is the pre-sentence resource quota every seeded agent starts from.
// A nil *QuotaSpec in Config means DefaultQuotaSpec.
type QuotaSpec struct {
	CPUMillis    int   `json:"cpuMillis"`
	MemoryBytes  int64 `json:"memoryBytes"`
	StorageBytes int64 `json:"storageBytes"`
	NetworkOpen  bool  `json:"networkOpen"`
}

// DefaultQuotaSpec is the 1.9.3 base quota (1000 m CPU / 4 GiB / 10 GiB / open).
func DefaultQuotaSpec() QuotaSpec {
	return QuotaSpec{CPUMillis: 1000, MemoryBytes: 4 << 30, StorageBytes: 10 << 30, NetworkOpen: true}
}

// Defaults for the two thresholds that used to be package constants.
const (
	DefaultCustodyThreshold = 2
	DefaultMonitorThreshold = 10
	// DefaultRetainEvents bounds how many audit events stay fully resident in
	// the judgement chain. Older events keep their digest/link metadata only.
	DefaultRetainEvents = 1024

	minCustodyThreshold = 2
	maxCustodyThreshold = 3

	minMonitorThreshold = 1
	maxMonitorThreshold = 1000
)

// MandatorySeats returns the four charter-mandated committee seats in their
// canonical order. The returned slice is a copy.
func MandatorySeats() []domain.CommitteeRole {
	return []domain.CommitteeRole{
		domain.SeatSafetyOrg,
		domain.SeatPrincipal,
		domain.SeatDeployer,
		domain.SeatModelProvider,
	}
}

// AllSeats returns every committee role the charter knows, mandatory first.
func AllSeats() []domain.CommitteeRole {
	return append(MandatorySeats(), domain.SeatAffectedNode)
}

// Config builds a System. The zero value means "the 1.9.3 defaults".
type Config struct {
	// Clock overrides the fixed clock; nil uses DefaultClock.
	Clock func() time.Time
	// Store, when non-nil, enables real crash-safe persistence (see persist.go).
	// nil keeps the pure in-memory behaviour.
	Store store.Store
	// Quota is the base resource quota seeded for every agent; nil means
	// DefaultQuotaSpec.
	Quota *QuotaSpec
	// CustodyThreshold is M in the simulated M-of-3 snapshot custody (2 or 3);
	// zero means DefaultCustodyThreshold.
	CustodyThreshold int
	// MonitorThreshold is the cumulative signal severity at which the behaviour
	// monitor freezes an agent (1..1000); zero means DefaultMonitorThreshold.
	MonitorThreshold int
	// Seats is the full committee roster. It must contain the four mandatory
	// seats; affected_node may be added. Empty means MandatorySeats().
	Seats []domain.CommitteeRole
	// CharterSeats amends which seats are mandatory. Empty keeps the four
	// charter seats; otherwise it must be a strict subset of them with at least
	// two seats, and CharterReason must explain the removal. The committee in
	// Seats must then contain every amended charter seat.
	CharterSeats []domain.CommitteeRole
	// CharterReason records why a charter seat was removed. It is required
	// exactly when CharterSeats narrows the default charter.
	CharterReason string
	// RetainEvents bounds the resident audit-chain window. 0 means
	// DefaultRetainEvents; -1 means keep every event resident (unbounded).
	RetainEvents int
}

// Validate rejects values outside the documented safety bounds. It is intended
// for user-facing front ends (the CLI) so a bad flag fails loudly instead of
// silently changing governance behaviour.
func (c Config) Validate() error {
	if c.Quota != nil {
		q := *c.Quota
		if q.CPUMillis < 0 {
			return fmt.Errorf("quota cpuMillis must be >= 0, got %d", q.CPUMillis)
		}
		if q.MemoryBytes <= 0 {
			return fmt.Errorf("quota memoryBytes must be > 0, got %d", q.MemoryBytes)
		}
		if q.StorageBytes <= 0 {
			return fmt.Errorf("quota storageBytes must be > 0, got %d", q.StorageBytes)
		}
	}
	if c.CustodyThreshold != 0 && (c.CustodyThreshold < minCustodyThreshold || c.CustodyThreshold > maxCustodyThreshold) {
		return fmt.Errorf("custody threshold must be %d..%d (M-of-3), got %d", minCustodyThreshold, maxCustodyThreshold, c.CustodyThreshold)
	}
	if c.MonitorThreshold != 0 && (c.MonitorThreshold < minMonitorThreshold || c.MonitorThreshold > maxMonitorThreshold) {
		return fmt.Errorf("monitor threshold must be %d..%d, got %d", minMonitorThreshold, maxMonitorThreshold, c.MonitorThreshold)
	}
	if len(c.CharterSeats) == 0 && strings.TrimSpace(c.CharterReason) != "" {
		return fmt.Errorf("charter reason given but no charter seat was removed")
	}
	if len(c.CharterSeats) > 0 {
		if err := validateCharterSeats(c.CharterSeats, c.CharterReason); err != nil {
			return err
		}
	}
	if len(c.Seats) > 0 {
		charter := c.CharterSeats
		if len(charter) == 0 {
			charter = MandatorySeats()
		}
		if err := validateSeats(c.Seats, charter); err != nil {
			return err
		}
	}
	if c.RetainEvents < -1 {
		return fmt.Errorf("retain events must be >= 0 (-1 = unbounded), got %d", c.RetainEvents)
	}
	return nil
}

// validateSeats rejects unknown/duplicated roles and requires every seat in the
// effective charter to be present exactly once.
func validateSeats(seats []domain.CommitteeRole, charter []domain.CommitteeRole) error {
	known := map[domain.CommitteeRole]bool{}
	for _, s := range AllSeats() {
		known[s] = true
	}
	seen := map[domain.CommitteeRole]bool{}
	for _, s := range seats {
		if !known[s] {
			return fmt.Errorf("unknown committee seat %q (known: %s)", s, joinRoles(AllSeats()))
		}
		if seen[s] {
			return fmt.Errorf("committee seat %q listed twice", s)
		}
		seen[s] = true
	}
	missing := []string{}
	for _, s := range charter {
		if !seen[s] {
			missing = append(missing, string(s))
		}
	}
	if len(missing) > 0 {
		if len(charter) == len(MandatorySeats()) {
			return fmt.Errorf("the four charter seats are mandatory and cannot be removed; missing: %s", strings.Join(missing, ", "))
		}
		return fmt.Errorf("the amended charter requires these seats; missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

// validateCharterSeats validates an amended charter: a non-empty strict subset
// of the four charter seats, at least two seats, no duplicates, and a reason
// whenever a seat is actually removed.
func validateCharterSeats(charter []domain.CommitteeRole, reason string) error {
	mandatory := map[domain.CommitteeRole]bool{}
	for _, s := range MandatorySeats() {
		mandatory[s] = true
	}
	seen := map[domain.CommitteeRole]bool{}
	for _, s := range charter {
		if !mandatory[s] {
			return fmt.Errorf("charter seat %q is not one of the four charter seats (%s)", s, joinRoles(MandatorySeats()))
		}
		if seen[s] {
			return fmt.Errorf("charter seat %q listed twice", s)
		}
		seen[s] = true
	}
	if len(charter) < 2 {
		return fmt.Errorf("a charter amendment must keep at least 2 seats, got %d", len(charter))
	}
	if len(charter) < len(MandatorySeats()) && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("removing a charter seat requires a non-empty reason")
	}
	if len(charter) == len(MandatorySeats()) && strings.TrimSpace(reason) != "" {
		return fmt.Errorf("charter reason given but no charter seat was removed")
	}
	return nil
}

// Resolve fills every unset field with its default and copies the caller's
// slices so later mutation cannot leak into the System. Out-of-range thresholds
// are clamped (Validate is the strict path used by front ends).
func (c Config) Resolve() Config {
	if c.Clock == nil {
		c.Clock = DefaultClock
	}
	if c.Quota == nil {
		q := DefaultQuotaSpec()
		c.Quota = &q
	} else {
		q := *c.Quota
		c.Quota = &q
	}
	if c.CustodyThreshold == 0 {
		c.CustodyThreshold = DefaultCustodyThreshold
	} else if c.CustodyThreshold < minCustodyThreshold {
		c.CustodyThreshold = minCustodyThreshold
	} else if c.CustodyThreshold > maxCustodyThreshold {
		c.CustodyThreshold = maxCustodyThreshold
	}
	if c.MonitorThreshold == 0 {
		c.MonitorThreshold = DefaultMonitorThreshold
	} else if c.MonitorThreshold < minMonitorThreshold {
		c.MonitorThreshold = minMonitorThreshold
	} else if c.MonitorThreshold > maxMonitorThreshold {
		c.MonitorThreshold = maxMonitorThreshold
	}
	if len(c.CharterSeats) == 0 {
		c.CharterSeats = MandatorySeats()
	} else {
		c.CharterSeats = append([]domain.CommitteeRole(nil), c.CharterSeats...)
	}
	if len(c.Seats) == 0 {
		c.Seats = append([]domain.CommitteeRole(nil), c.CharterSeats...)
	} else {
		c.Seats = append([]domain.CommitteeRole(nil), c.Seats...)
	}
	switch {
	case c.RetainEvents == 0:
		c.RetainEvents = DefaultRetainEvents
	case c.RetainEvents < -1:
		c.RetainEvents = DefaultRetainEvents
	}
	return c
}

// Seats returns a copy of the resolved committee roster.
func (c Config) resolvedSeats() []domain.CommitteeRole {
	return append([]domain.CommitteeRole(nil), c.Seats...)
}

func joinRoles(roles []domain.CommitteeRole) string {
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		parts = append(parts, string(r))
	}
	return strings.Join(parts, ", ")
}
