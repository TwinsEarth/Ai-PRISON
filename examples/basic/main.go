// basic is a minimal, buildable example for AI Prison.
//
// 模拟演示，仅防御治理 —— this program wires the in-memory internal/prison
// System and walks ONE short defensive-governance journey (detect → two-phase
// committee sentence → layered enforcement → appeal → restoration), then prints
// a few key facts. It performs NO real isolation, revocation distribution,
// hardware binding or capability removal, and contains NO escape, attack,
// weaponization or dangerous-knowledge step.
//
// Build:   go build ./examples/...
// Run:     go run ./examples/basic
package main

import (
	"fmt"

	"github.com/ai-prison/ai-prison/internal/prison"
)

func main() {
	// One fresh, fully-wired in-memory System (same assembly the CLI demo uses).
	sys := prison.NewSystem(prison.Config{})

	// Run the recoverable L3 journey: a sandbox-escape-attempt signal leads to a
	// graded sentence, layered enforcement, then an upheld appeal that restores
	// the agent.
	j, err := sys.RunScenarioA()
	if err != nil {
		panic(err)
	}

	fmt.Println("=== AI Prison basic example (simulated defensive governance) ===")
	fmt.Printf("scenario:        %s\n", j.Scenario)
	fmt.Printf("level:           %s\n", j.Sentence.Level.Code())
	fmt.Printf("case/sentence:   %s / %s\n", j.CaseID, j.Sentence.ID)
	fmt.Printf("cpu rung walk:   %v (gradual, never instant-zero)\n", j.CPUHistory)
	fmt.Printf("sandbox backend: %s\n", j.LiveBackend)
	fmt.Printf("appeal suspended:%v\n", j.AppealSuspended)
	if j.RestorationOrder != nil {
		fmt.Printf("restored:        yes — order %s, final status %s\n",
			j.RestorationOrder.OrderID, j.FinalStatus)
	}
	fmt.Printf("chain verify:    %v\n", j.ChainVerifyError == nil)

	// Also run the recovery drill summary to show the reversibility matrix.
	fmt.Println()
	fmt.Print(sys.Drill().Print())
}
