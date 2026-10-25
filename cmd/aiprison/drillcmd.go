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
// dataDir is set the drill's governance record is projected onto a real
// FileStore ledger.
func runDrill(dataDir string) {
	var rep *prison.DrillReport
	var recordCount, segCount int
	var dir string

	if dataDir == "" {
		rep = prison.RunRecoveryDrill()
	} else {
		dir = dataDir
		fmt.Printf("数据目录: %s\n", dataDir)
		fs, err := store.NewFileStore(dataDir, store.Options{Clock: prison.DefaultClock})
		if err != nil {
			fail(fmt.Errorf("open data dir: %w", err))
		}
		sys := prison.NewSystem(prison.Config{Store: fs})
		if sys.BootstrapErr != nil {
			fail(sys.BootstrapErr)
		}
		rep = sys.Drill()
		if err := sys.Close(); err != nil {
			fail(err)
		}
		recordCount = fs.Len()
		segCount = prison.SegmentCount(dataDir)
	}

	fmt.Print(rep.Print())
	if dataDir != "" {
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
