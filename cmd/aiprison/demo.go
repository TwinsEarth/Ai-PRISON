package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/prison"
	"github.com/ai-prison/ai-prison/internal/store"
)

// runDemo executes both end-to-end scenarios on a fresh System and prints a
// segmented, structured Chinese trace. Everything below is a SIMULATED
// governance flow: no real isolation, revocation distribution, hardware binding
// or capability removal happens.
//
// When dataDir is non-empty the governance/audit record is projected onto a
// real crash-safe FileStore ledger under dataDir (and fsynced on exit); without
// it the demo runs purely in memory exactly as v1.0.
func runDemo(dataDir string) {
	sys, close := newSystemFor(dataDir)

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

	if err := close(); err != nil {
		fail(err)
	}
}

// newSystemFor builds a System, wiring a FileStore ledger when dataDir is set.
// The returned close func flushes and closes the ledger (no-op in-memory).
func newSystemFor(dataDir string) (*prison.System, func() error) {
	if dataDir == "" {
		return prison.NewSystem(prison.Config{}), func() error { return nil }
	}
	fmt.Printf("数据目录: %s\n", dataDir)
	fs, err := store.NewFileStore(dataDir, store.Options{
		// Deterministic record timestamps; batching with Close as the
		// durability boundary is the right trade for the demo.
		Clock: prison.DefaultClock,
	})
	if err != nil {
		fail(fmt.Errorf("open data dir: %w", err))
	}
	sys := prison.NewSystem(prison.Config{Store: fs})
	if sys.BootstrapErr != nil {
		fail(sys.BootstrapErr)
	}
	n := fs.Len()
	seg := prison.SegmentCount(dataDir)
	if n > 0 {
		fmt.Printf("(reopened ledger: %d records / %d segments replayed)\n", n, seg)
	}
	return sys, func() error {
		if err := sys.Close(); err != nil {
			return err
		}
		fmt.Println("── 落盘摘要 ──")
		fmt.Printf("  已落盘记录数:  %d\n", fs.Len())
		fmt.Printf("  段数:          %d\n", prison.SegmentCount(dataDir))
		return nil
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "failed:", err)
	os.Exit(1)
}
