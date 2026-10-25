package migration

import (
	"fmt"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Service orchestrates the three migration-suppression measures according to
// the disposition level. Dependencies are small interfaces so tests can use
// fakes while production wires the in-memory implementations below.
type Service struct {
	Binder  HardwareBinder
	Records RecordKeeper
	Custody CustodyStore
	Events  EventSink
	Now     func() time.Time
}

// Config builds a Service; nil fields fall back to safe in-memory defaults /
// no-op sinks.
type Config struct {
	Binder  HardwareBinder
	Records RecordKeeper
	Custody CustodyStore
	Events  EventSink
}

// NewService returns a wired orchestrator.
func NewService(cfg Config) *Service {
	s := &Service{
		Binder:  cfg.Binder,
		Records: cfg.Records,
		Custody: cfg.Custody,
		Events:  cfg.Events,
		Now:     time.Now,
	}
	if s.Binder == nil {
		s.Binder = NewMemHardwareBinder()
	}
	if s.Records == nil {
		s.Records = NewMemRecordKeeper()
	}
	if s.Custody == nil {
		s.Custody = NewMemCustodyStore(s.Events)
	}
	if s.Events == nil {
		s.Events = noopEvents{}
	}
	return s
}

// SuppressMigration applies the measures the level profile mandates, in order:
//
//   - L3+ (hardware binding): bind the listed capability credentials to the
//     simulated fingerprint; a copy on another machine fails validation.
//   - L5 (record anchor): revoke the agent's portable-record anchor key, so a
//     migrated copy cannot inherit reputation/history (ZeroReputation).
//   - L5 (snapshot custody): seal the environment snapshot under M-of-N
//     multi-party custody (SealSnapshot profile flag).
//
// The call is idempotent: re-running for the same agent/level does not mint
// duplicate bindings, double-revoke (a delete twice is harmless) or re-seal.
func (s *Service) SuppressMigration(in SuppressInput) (*SuppressReport, error) {
	if in.AgentID == "" {
		return nil, fmt.Errorf("migration: suppress needs an agent id")
	}
	prof, err := in.Level.Profile()
	if err != nil {
		return nil, err
	}

	rep := &SuppressReport{SentenceID: in.SentenceID, AgentID: in.AgentID, Level: in.Level}

	// Path 1 — hardware binding (L3 and above).
	if in.Level >= domain.L3 {
		for _, credID := range in.CapabilityCredIDs {
			if err := s.Binder.Bind(credID, in.AgentID, in.Fingerprint); err != nil {
				return nil, err
			}
			rep.BoundCredentials = append(rep.BoundCredentials, credID)
		}
		if len(rep.BoundCredentials) > 0 {
			rep.Applied = append(rep.Applied, MeasureHardwareBinding)
		}
	}

	// Path 2 — revoke the portable work-record anchor key (L5).
	if prof.ZeroReputation || prof.RevokeIdentity {
		if in.RecordAnchorKey != "" {
			s.Records.RevokeAnchor(in.RecordAnchorKey)
		}
		rep.Applied = append(rep.Applied, MeasureRecordKeyRevoke)
	}

	// Path 3 — multi-party snapshot custody (L5 SealSnapshot).
	if prof.SealSnapshot {
		id := "custody-" + string(in.AgentID)
		sealed, err := s.Custody.Seal(id, in.AgentID, in.SnapshotContentHash, in.UnsealThreshold, in.Custodians)
		if err != nil {
			return nil, err
		}
		rep.Custody = sealed
		rep.Applied = append(rep.Applied, MeasureSnapshotCustody)
	}

	s.Events.Append(domain.Event{
		Type:       domain.EventMigrationSuppressed,
		AgentID:    in.AgentID,
		SentenceID: in.SentenceID,
		Actor:      "migration-suppression",
		Payload: map[string]string{
			"level":     in.Level.Code(),
			"applied":   joinStrings(rep.Applied),
			"simulated": "true",
		},
	})
	return rep, nil
}

// joinStrings renders a measure list as a comma-joined payload string.
func joinStrings(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// noopEvents is the safe default when no EventSink is wired.
type noopEvents struct{}

func (noopEvents) Append(ev domain.Event) domain.Event { return ev }
