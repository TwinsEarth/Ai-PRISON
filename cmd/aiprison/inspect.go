package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/prison"
	"github.com/ai-prison/ai-prison/internal/store"
)

// runInspect opens the ledger read-only, replays it onto a fresh System and
// prints the structured InspectView. It never writes anything to the data dir.
func runInspect(dataDir string) {
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "inspect: --data-dir is required")
		os.Exit(2)
	}
	ro, err := store.OpenReadonly(dataDir)
	if err != nil {
		printIntegrityFailure(err)
		os.Exit(1)
	}
	sys := prison.NewSystem(prison.Config{Store: ro})
	_ = ro.Close()
	if sys.BootstrapErr != nil {
		fmt.Fprintln(os.Stderr, "inspect: replay failed:", sys.BootstrapErr)
		os.Exit(1)
	}

	v := sys.InspectView()
	fmt.Println("═══ ledger inspect (read-only replay) ═══")
	fmt.Printf("  数据目录:        %s\n", v.DataDir)
	fmt.Printf("  记录数/段数:      %d / %d\n", v.StoreRecords, prison.SegmentCount(dataDir))
	fmt.Printf("  链事件数:        %d\n", v.ChainEvents)
	if v.ChainVerifyOK {
		fmt.Println("  CHAIN VERIFY:    OK")
	} else {
		fmt.Printf("  CHAIN VERIFY:    FAIL: %s\n", v.ChainVerifyNote)
	}
	fmt.Printf("  吊销条目:        %d (enforced=%d)\n", v.RevocationEntries, v.RevocationEnforced)
	if len(v.UntrustedNodes) == 0 {
		fmt.Println("  不可信节点:      (none)")
	} else {
		fmt.Printf("  不可信节点:      %v\n", v.UntrustedNodes)
	}
	fmt.Printf("  回执:            acked=%d open=%d\n", v.ReceiptsAcked, v.ReceiptsOpen)
	fmt.Printf("  判决/案件:       %d / %d\n", v.Sentences, v.Cases)

	if len(v.Snapshots) == 0 {
		fmt.Println("  快照元数据:      (none)")
	} else {
		fmt.Println("  快照元数据:")
		for _, s := range v.Snapshots {
			fmt.Printf("    - %s agent=%s hash=%s\n", s.ID, s.AgentID, shortHash(s.ContentHash))
		}
	}

	fmt.Println("  重建 agents:")
	for _, a := range v.Agents {
		fmt.Printf("    - %-16s level=%-3s status=%-16s cardRevoked=%-5v reputation=%-4d did=%s\n",
			a.AgentID, a.Level, a.Status, a.CardRevoked, a.Reputation, a.DID)
	}
}

// runVerify only integrity-checks the ledger: INTEGRITY OK + exit 0 on a
// healthy chain, otherwise locate the fault and exit 1.
func runVerify(dataDir string) {
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "verify: --data-dir is required")
		os.Exit(2)
	}
	ro, err := store.OpenReadonly(dataDir)
	if err != nil {
		printIntegrityFailure(err)
		os.Exit(1)
	}
	n := ro.Len()
	_ = ro.Close()
	fmt.Printf("INTEGRITY OK — %d records, per-record hash chain verified\n", n)
	os.Exit(0)
}

// runRepairTail only truncates a genuine torn (incomplete) tail line; it
// refuses to touch a complete but tampered record ("hard tamper").
func runRepairTail(dataDir string) {
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "repair-tail: --data-dir is required")
		os.Exit(2)
	}
	if _, err := store.OpenReadonly(dataDir); err == nil {
		fmt.Println("ledger 完整: 无 torn tail, 无需修复 (零改动)")
		return
	} else {
		if store.IsTamper(err) {
			fmt.Fprintln(os.Stderr, "hard tamper, refusing to repair:")
			printIntegrityFailure(err)
			os.Exit(1)
		}
		if !store.IsTornTail(err) {
			fmt.Fprintln(os.Stderr, "open ledger failed (not a torn tail):")
			printIntegrityFailure(err)
			os.Exit(1)
		}
	}
	removed, file, err := store.RepairTornTail(dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "repair-tail:", err)
		os.Exit(1)
	}
	// Re-open to prove the repaired ledger verifies.
	ro, err := store.OpenReadonly(dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "repair-tail: post-repair verification failed:")
		printIntegrityFailure(err)
		os.Exit(1)
	}
	n := ro.Len()
	_ = ro.Close()
	fmt.Printf("torn tail 已修复: 从 %s 移除 %d 字节, 修复后记录数=%d, INTEGRITY OK\n", file, removed, n)
}

// printIntegrityFailure renders an *store.IntegrityError with its file/seq/line
// location exactly where the fault was found.
func printIntegrityFailure(err error) {
	if ie := store.AsIntegrityError(err); ie != nil {
		fmt.Fprintf(os.Stderr, "INTEGRITY FAILURE [%s]: %s\n", ie.Kind, ie.Detail)
		fmt.Fprintf(os.Stderr, "  file=%s line=%d seq=%d offset=%d\n", ie.File, ie.Line, ie.Seq, ie.Offset)
		return
	}
	fmt.Fprintln(os.Stderr, "ledger error:", err)
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}
