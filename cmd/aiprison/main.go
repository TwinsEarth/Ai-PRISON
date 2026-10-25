// Command aiprison is the CLI for the AI Prison governance prototype.
// The end-to-end demo is implemented in v0.9.5; earlier versions expose
// version and grading information.
package main

import (
	"fmt"
	"os"

	"github.com/ai-prison/ai-prison/internal/domain"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}
	switch args[0] {
	case "version":
		fmt.Println("aiprison", version)
	case "levels":
		printLevels()
	case "demo":
		// Runs both end-to-end scenarios on an in-memory System (see demo.go).
		runDemo()
	case "drill":
		// Runs the docs/06 recovery-drill matrix on a fresh System (see drillcmd.go).
		runDrill()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		printUsage()
		os.Exit(2)
	}
}

const version = "1.0.0"

func printUsage() {
	fmt.Println("aiprison — defensive AI-agent governance prototype")
	fmt.Println("usage: aiprison <command>")
	fmt.Println("commands:")
	fmt.Println("  version   print the version")
	fmt.Println("  levels    print the L0–L5 grading matrix")
	fmt.Println("  demo      run the end-to-end demo (v0.9.5)")
	fmt.Println("  drill     run the v1.0.0 recovery drill (docs/06 matrix, PASS/FAIL report)")
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
