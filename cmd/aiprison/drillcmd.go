package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/prison"
	"github.com/ai-prison/ai-prison/internal/store"
)

// runDrill executes the v1.0.0 recovery-drill matrix on a fresh System and
// prints the structured PASS/FAIL report. It exits non-zero if any drill
// assertion fails (docs/06 §4.2 — a single failing assertion FAILS the drill).
//
// Everything below is a SIMULATED governance drill: no real isolation, GRAM/TLM
// reload, token stack, hardware binding or identity unseal happens. When
// --data-dir is set the drill's governance record is projected onto a real
// FileStore ledger.
func runDrill(o options) {
	var rep *prison.DrillReport
	var recordCount, segCount int
	var dir string

	clk, err := o.resolveClock()
	if err != nil {
		fail(err)
	}
	cfg, err := o.prisonConfig(clk, nil)
	if err != nil {
		fail(err)
	}

	if o.dataDir == "" {
		rep = prison.RunRecoveryDrillConfig(cfg)
	} else {
		dir = o.dataDir
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
		rep = sys.Drill()
		if err := sys.Close(); err != nil {
			fail(err)
		}
		recordCount = fs.Len()
		segCount = prison.SegmentCount(o.dataDir)
	}

	fmt.Print(rep.Print())
	if o.dataDir != "" {
		fmt.Println()
		fmt.Println("── 落盘摘要 ──")
		fmt.Printf("  数据目录:      %s\n", dir)
		fmt.Printf("  已落盘记录数:  %d\n", recordCount)
		fmt.Printf("  段数:          %d\n", segCount)
	}
	if !rep.Overall {
		os.Exit(1)
	}
}
