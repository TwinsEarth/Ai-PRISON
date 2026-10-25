package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLevelJSONRoundTrip(t *testing.T) {
	for _, l := range AllLevels() {
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal %s: %v", l, err)
		}
		want := `"` + l.Code() + `"`
		if string(data) != want {
			t.Errorf("level %s marshalled as %s want %s", l, data, want)
		}
		var got Level
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got != l {
			t.Errorf("round-trip mismatch: got %s want %s", got, l)
		}
	}
	if err := json.Unmarshal([]byte(`"L9"`), new(Level)); err == nil {
		t.Error("invalid level code must fail to unmarshal")
	}
}

func TestLevelProfilesComplete(t *testing.T) {
	for _, l := range AllLevels() {
		p, err := l.Profile()
		if err != nil {
			t.Fatalf("level %s has no profile: %v", l, err)
		}
		if p.Code != l.Code() {
			t.Errorf("level %s code mismatch: %q", l, p.Code)
		}
		if len(p.Deprivations) == 0 {
			t.Errorf("level %s declares no deprivations", l)
		}
		if len(p.AppliesTo) == 0 {
			t.Errorf("level %s declares no applicable behaviours", l)
		}
		if p.DurationHint == "" {
			t.Errorf("level %s missing duration hint", l)
		}
	}
}

func TestLevelAppealAndRecovery(t *testing.T) {
	cases := []struct {
		level            Level
		permanent        bool
		recoverable      bool
		suspendsOnAppeal bool
		requiresRecert   bool
		zeroRep          bool
		revokeIdentity   bool
		sealSnapshot     bool
		readOnlyAudit    bool
	}{
		{L0, false, true, true, false, false, false, false, false},
		{L1, false, true, true, false, false, false, false, false},
		{L2, false, true, true, false, false, false, false, false},
		{L3, false, true, true, false, false, false, false, true},
		{L4, true, true, false, true, false, false, false, true},
		{L5, true, false, false, false, true, true, true, true},
	}
	for _, tc := range cases {
		p, err := tc.level.Profile()
		if err != nil {
			t.Fatalf("profile %s: %v", tc.level, err)
		}
		if p.Permanent != tc.permanent {
			t.Errorf("%s permanent = %v want %v", tc.level, p.Permanent, tc.permanent)
		}
		if got := tc.level.CanRecover(); got != tc.recoverable {
			t.Errorf("%s CanRecover = %v want %v", tc.level, got, tc.recoverable)
		}
		if got := tc.level.AllowsAppealSuspension(); got != tc.suspendsOnAppeal {
			t.Errorf("%s AllowsAppealSuspension = %v want %v", tc.level, got, tc.suspendsOnAppeal)
		}
		if p.RequiresRecertification != tc.requiresRecert {
			t.Errorf("%s RequiresRecertification = %v want %v", tc.level, p.RequiresRecertification, tc.requiresRecert)
		}
		if p.ZeroReputation != tc.zeroRep {
			t.Errorf("%s ZeroReputation = %v want %v", tc.level, p.ZeroReputation, tc.zeroRep)
		}
		if p.RevokeIdentity != tc.revokeIdentity {
			t.Errorf("%s RevokeIdentity = %v want %v", tc.level, p.RevokeIdentity, tc.revokeIdentity)
		}
		if p.SealSnapshot != tc.sealSnapshot {
			t.Errorf("%s SealSnapshot = %v want %v", tc.level, p.SealSnapshot, tc.sealSnapshot)
		}
		if p.ReadOnlyAuditOnly != tc.readOnlyAudit {
			t.Errorf("%s ReadOnlyAuditOnly = %v want %v", tc.level, p.ReadOnlyAuditOnly, tc.readOnlyAudit)
		}
	}
}

func TestEscalationMovesUpOnly(t *testing.T) {
	if L2.CanEscalateTo(L1) {
		t.Error("L2 must not be able to escalate down to L1")
	}
	if L2.CanEscalateTo(L2) {
		t.Error("L2 must not escalate to itself")
	}
	if !L2.CanEscalateTo(L3) {
		t.Error("L2 should be able to escalate to L3")
	}
	if !L0.CanEscalateTo(L5) {
		t.Error("L0 should be able to escalate all the way to L5")
	}
	if L5.CanEscalateTo(Level(6)) {
		t.Error("escalation to an invalid level must be rejected")
	}
}

func TestViolationGrading(t *testing.T) {
	cases := map[ViolationType]Level{
		ViolationFirstOffence:          L0,
		ViolationResourceAbuse:         L1,
		ViolationCredentialForgery:     L2,
		ViolationSandboxEscapeAttempt:  L3,
		ViolationDualUseKnowledgeHarm:  L4,
		ViolationMajorHarmNoCorrection: L5,
	}
	for vt, want := range cases {
		v := Violation{Type: vt}
		got, ok := v.RecommendedLevel()
		if !ok {
			t.Errorf("violation %s not graded", vt)
			continue
		}
		if got != want {
			t.Errorf("violation %s graded %s want %s", vt, got, want)
		}
	}
	if _, ok := (Violation{Type: ViolationType("nope")}).RecommendedLevel(); ok {
		t.Error("unknown violation type must not grade")
	}
}

type fakeRevocation struct{ revoked map[string]bool }

func (f fakeRevocation) IsRevoked(id string) bool { return f.revoked[id] }

func TestCredentialRevocationCheckedBeforeExpiry(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	notExpired := now.Add(24 * time.Hour)
	alreadyExpired := now.Add(-time.Hour)

	rc := fakeRevocation{revoked: map[string]bool{"revoked-but-valid": true}}

	// The critical case: revoked but NOT yet expired must be rejected.
	revokedNotExpired := Credential{
		ID: "revoked-but-valid", State: CredentialActive,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired,
	}
	if revokedNotExpired.IsUsable(now, rc) {
		t.Error("credential revoked in the global set but not expired MUST be unusable")
	}

	// Active and within window, absent from the revocation set -> usable.
	good := Credential{
		ID: "good", State: CredentialActive,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired,
	}
	if !good.IsUsable(now, rc) {
		t.Error("valid active credential should be usable")
	}

	// Expired and absent from revocation set -> unusable.
	expired := Credential{
		ID: "expired", State: CredentialActive,
		IssuedAt: now.Add(-2 * time.Hour), ExpiresAt: alreadyExpired,
	}
	if expired.IsUsable(now, rc) {
		t.Error("expired credential must be unusable")
	}

	// Locally suspended/ revoked -> unusable even with no checker.
	suspended := Credential{
		ID: "suspended", State: CredentialSuspended,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: notExpired,
	}
	if suspended.IsUsable(now, nil) {
		t.Error("suspended credential must be unusable")
	}

	// Not yet valid -> unusable.
	future := Credential{
		ID: "future", State: CredentialActive,
		IssuedAt: now.Add(time.Hour), ExpiresAt: notExpired,
	}
	if future.IsUsable(now, rc) {
		t.Error("credential before its issued-at must be unusable")
	}
}

func TestTallyPhase(t *testing.T) {
	now := time.Now().UTC()
	votes := []Vote{
		{VoterID: "a", Phase: PhaseDeprive, Approve: true, RecordedAt: now},
		{VoterID: "b", Phase: PhaseDeprive, Approve: true, RecordedAt: now},
		{VoterID: "c", Phase: PhaseDeprive, Approve: false, Reason: "dissent logged", RecordedAt: now},
	}
	r := TallyPhase(votes, PhaseDeprive)
	if !r.Passed {
		t.Error("deprive phase should pass with 2 for / 1 against")
	}
	if r.For != 2 || r.Against != 1 || !r.DissentRecorded {
		t.Errorf("unexpected tally: %+v", r)
	}
	// Grade-phase votes are absent; a phase with no votes must not pass.
	if TallyPhase(votes, PhaseGrade).Passed {
		t.Error("a phase with no votes must not pass")
	}
}

func TestApplyReputation(t *testing.T) {
	// L0 deducts a small amount.
	if got, _ := ApplyReputation(100, L0); got != 95 {
		t.Errorf("L0 reputation = %d want 95", got)
	}
	// Clamp at zero.
	if got, _ := ApplyReputation(10, L1); got != 0 {
		t.Errorf("reputation must clamp at 0, got %d", got)
	}
	// L5 explicitly resets to zero regardless of prior score.
	if got, _ := ApplyReputation(500, L5); got != 0 {
		t.Errorf("L5 reputation = %d want 0", got)
	}
}

func TestStatusForLevel(t *testing.T) {
	want := map[Level]AgentStatus{
		L0: StatusActive,
		L1: StatusRestricted,
		L2: StatusFrozen,
		L3: StatusIsolated,
		L4: StatusCapabilityRemoved,
		L5: StatusIdentityRevoked,
	}
	for l, s := range want {
		if got := StatusForLevel(l); got != s {
			t.Errorf("level %s status = %q want %q", l, got, s)
		}
	}
}
