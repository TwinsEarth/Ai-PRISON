package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/prison"
)

// runDrill executes the v1.0.0 recovery-drill matrix on a fresh in-memory System
// and prints the structured PASS/FAIL report. It exits non-zero if any drill
// assertion fails (docs/06 §4.2 — a single failing assertion FAILS the drill).
//
// Everything below is a SIMULATED governance drill: no real isolation, GRAM/TLM
// reload, token stack, hardware binding or identity unseal happens.
func runDrill() {
	rep := prison.RunRecoveryDrill()
	fmt.Print(rep.Print())
	if !rep.Overall {
		os.Exit(1)
	}
}
