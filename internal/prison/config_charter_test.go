package prison

import (
	"strings"
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
)

var (
	seatSafety   = domain.SeatSafetyOrg
	seatPrincipal = domain.SeatPrincipal
	seatDeployer = domain.SeatDeployer
	seatProvider = domain.SeatModelProvider
)

func TestCharterConfigDefaults(t *testing.T) {
	cfg := Config{}.Resolve()
	if len(cfg.CharterSeats) != 4 || len(cfg.Seats) != 4 {
		t.Fatalf("defaults: charter=%v seats=%v", cfg.CharterSeats, cfg.Seats)
	}
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("zero config must validate: %v", err)
	}
}

func TestCharterConfigAmendment(t *testing.T) {
	cfg := Config{
		CharterSeats:  []domain.CommitteeRole{seatSafety, seatPrincipal, seatDeployer},
		CharterReason: "model provider seat vacated by charter review",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid amendment rejected: %v", err)
	}
	resolved := cfg.Resolve()
	if len(resolved.Seats) != 3 || len(resolved.CharterSeats) != 3 {
		t.Fatalf("resolved amendment: charter=%v seats=%v", resolved.CharterSeats, resolved.Seats)
	}

	// A wider rosters still has to carry every amended charter seat.
	withRoster := Config{
		CharterSeats:  []domain.CommitteeRole{seatSafety, seatPrincipal},
		CharterReason: "two-party emergency charter",
		Seats:         []domain.CommitteeRole{seatSafety, seatPrincipal, domain.SeatAffectedNode},
	}
	if err := withRoster.Validate(); err != nil {
		t.Fatalf("roster with extra affected_node rejected: %v", err)
	}
}

func TestCharterConfigRejections(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"removal without reason", Config{CharterSeats: []domain.CommitteeRole{seatSafety, seatPrincipal}}, "requires a non-empty reason"},
		{"single seat", Config{CharterSeats: []domain.CommitteeRole{seatSafety}, CharterReason: "x"}, "at least 2 seats"},
		{"non-charter seat", Config{CharterSeats: []domain.CommitteeRole{seatSafety, domain.SeatAffectedNode}, CharterReason: "x"}, "is not one of the four charter seats"},
		{"duplicate", Config{CharterSeats: []domain.CommitteeRole{seatSafety, seatSafety}, CharterReason: "x"}, "listed twice"},
		{"reason without removal", Config{CharterReason: "x"}, "no charter seat was removed"},
		{"roster misses amended seat", Config{
			CharterSeats:  []domain.CommitteeRole{seatSafety, seatPrincipal},
			CharterReason: "x",
			Seats:         []domain.CommitteeRole{seatSafety, seatDeployer},
		}, "amended charter requires these seats"},
		{"default roster drops seat", Config{
			Seats: []domain.CommitteeRole{seatSafety, seatPrincipal, seatDeployer},
		}, "the four charter seats are mandatory"},
	}
	for _, tc := range cases {
		err := tc.cfg.Validate()
		if err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v want substring %q", tc.name, err, tc.want)
		}
	}
}

func TestCharterConfigRosterValidationUnchanged(t *testing.T) {
	// The four-seat default path must keep exactly the v1.9.4 error semantics.
	cfg := Config{Seats: []domain.CommitteeRole{seatSafety, seatPrincipal, seatDeployer, seatProvider, domain.SeatAffectedNode}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("five-seat default roster rejected: %v", err)
	}
}
