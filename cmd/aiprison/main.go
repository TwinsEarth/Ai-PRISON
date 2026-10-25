// Command aiprison is the CLI for the AI Prison governance prototype.
package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func main() {
	dataDir, positional, ok := parseArgs(os.Args[1:])
	if !ok {
		os.Exit(2)
	}
	if len(positional) == 0 {
		printUsage()
		return
	}
	switch positional[0] {
	case "version":
		fmt.Println("aiprison", version)
	case "levels":
		printLevels()
	case "demo":
		runDemo(dataDir)
	case "drill":
		runDrill(dataDir)
	case "inspect":
		runInspect(dataDir)
	case "verify":
		runVerify(dataDir)
	case "repair-tail":
		runRepairTail(dataDir)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", positional[0])
		printUsage()
		os.Exit(2)
	}
}

const version = "1.0.0"

// parseArgs splits argv into the global --data-dir flag (accepted before OR
// after the subcommand: `aiprison --data-dir D demo` and
// `aiprison demo --data-dir D` are equivalent) and the remaining positional
// arguments. ok=false means a usage error has already been printed.
func parseArgs(argv []string) (dataDir string, positional []string, ok bool) {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--data-dir":
			if i+1 >= len(argv) {
				fmt.Fprintln(os.Stderr, "flag --data-dir requires a path argument")
				return "", nil, false
			}
			i++
			dataDir = argv[i]
		case hasPrefix(a, "--data-dir="):
			dataDir = a[len("--data-dir="):]
		default:
			positional = append(positional, a)
		}
	}
	return dataDir, positional, true
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func printUsage() {
	fmt.Println("aiprison — defensive AI-agent governance prototype")
	fmt.Println()
	fmt.Println("usage: aiprison [--data-dir DIR] <command> [--data-dir DIR]")
	fmt.Println()
	fmt.Println("global flags:")
	fmt.Println("  --data-dir DIR   durable governance ledger directory (default: pure in-memory)")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  version             print the version")
	fmt.Println("  levels              print the L0–L5 grading matrix")
	fmt.Println("  demo                run the end-to-end demo (v0.9.5; persists when --data-dir is set)")
	fmt.Println("  drill               run the v1.0.0 recovery drill (persists when --data-dir is set)")
	fmt.Println("  inspect             read-only replay of --data-dir: chain, agents, sentences, revocations, receipts, snapshots")
	fmt.Println("  verify              integrity-check --data-dir; INTEGRITY OK + exit 0, else locate tamper/torn tail + exit 1")
	fmt.Println("  repair-tail         remove only a genuine torn (incomplete) tail line; refuse hard tamper")
}

func printLevels() {
	for _, l := range domain.AllLevels() {
		p, err := l.Profile()
		if err != nil {
			continue
		}
		fmt.Printf("%s %-18s [%s] %s\n", p.Code, p.Name, p.DurationClass, p.DurationHint)
		for _, d := range p.Deprivations {
			fmt.Printf("    - %s\n", d)
		}
	}
}
