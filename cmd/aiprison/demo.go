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
// When --data-dir is non-empty the governance/audit record is projected onto a
// real crash-safe FileStore ledger under that directory (and fsynced on exit);
// without it the demo runs purely in memory exactly as before.
func runDemo(o options) {
	sys, close := newSystemFor(o)

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

// newSystemFor builds a System from the CLI options, wiring a FileStore ledger
// when --data-dir is set. The returned close func flushes and closes the ledger
// (no-op in-memory).
func newSystemFor(o options) (*prison.System, func() error) {
	clk, err := o.resolveClock()
	if err != nil {
		fail(err)
	}
	cfg, err := o.prisonConfig(clk, nil)
	if err != nil {
		fail(err)
	}
	if o.dataDir == "" {
		return prison.NewSystem(cfg), func() error { return nil }
	}
	fmt.Printf("数据目录: %s\n", o.dataDir)
	so, err := o.storeOptions(clk)
	if err != nil {
		fail(err)
	}
	fs, err := store.NewFileStore(o.dataDir, so)
	if err != nil {
		fail(fmt.Errorf("open data dir: %w", err))
	}
	cfg.Store = fs
	sys := prison.NewSystem(cfg)
	if sys.BootstrapErr != nil {
		fail(sys.BootstrapErr)
	}
	n := fs.Len()
	seg := prison.SegmentCount(o.dataDir)
	if n > 0 {
		fmt.Printf("(reopened ledger: %d records / %d segments replayed)\n", n, seg)
	}
	return sys, func() error {
		if err := sys.Close(); err != nil {
			return err
			}
		st := fs.Stats()
		fmt.Println("── 落盘摘要 ──")
		fmt.Printf("  已落盘记录数:  %d\n", st.TotalRecords)
		fmt.Printf("  段数:          %d\n", st.DiskSegments)
		fmt.Printf("  常驻热窗口:    %d 条（起始 seq=%d，上限 %d 段 × %d 条）\n",
			st.InMemory, st.BaseSeq, st.RetainSegments, st.SegmentSize)
		return nil
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "failed:", err)
	os.Exit(1)
}
