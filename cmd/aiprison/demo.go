package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/prison"
)

// runDemo executes both end-to-end scenarios on a fresh in-memory System and
// prints a segmented, structured Chinese trace. Everything below is a SIMULATED
// governance flow: no real isolation, revocation distribution, hardware binding
// or capability removal happens.
func runDemo() {
	sys := prison.NewSystem(prison.Config{})

	ja, err := sys.RunScenarioA()
	if err != nil {
		fail(err)
	}
	fmt.Print(ja.Print())
	fmt.Println()

	jb, err := sys.RunScenarioB()
	if err != nil {
		fail(err)
	}
	fmt.Print(jb.Print())

	fmt.Println()
	fmt.Println("注: 以上全程为内存模拟 —— 无真实隔离 / 吊销分发 / 硬件绑定 / 能力摘除，")
	fmt.Println("    仅用于演示防御性治理流程(检测→七阶段两阶段判决→分层执行→审计→申诉/矫正→恢复)。")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "demo failed:", err)
	os.Exit(1)
}
