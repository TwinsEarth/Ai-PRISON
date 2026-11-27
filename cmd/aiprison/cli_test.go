package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/store"
)

// captureCLI runs fn with os.Stdout/os.Stderr redirected to pipes. Tests are
// sequential (no t.Parallel) because the CLI writes through the process streams.
func captureCLI(t *testing.T, fn func() int) (code int, stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW
	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { b, _ := io.ReadAll(outR); outCh <- string(b) }()
	go func() { b, _ := io.ReadAll(errR); errCh <- string(b) }()
	code = fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return code, <-outCh, <-errCh
}

func tempCLIDir(t *testing.T, prefix string) string {
	t.Helper()
	base := os.Getenv("AIPRISON_TEST_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir, err := os.MkdirTemp(base, "cmd-"+prefix+"-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestParseArgsConfigurationSurface(t *testing.T) {
	args := []string{
		"--data-dir", "D:/ledger",
		"--segment-size=8",
		"--sync-every-append=false",
		"--file-mode", "0640",
		"--clock", "2020-01-01T00:00:00Z",
		"--quota-cpu", "250",
		"--quota-memory", "1G",
		"--quota-storage=2G",
		"--quota-network=false",
		"--custody-m", "3",
		"--monitor-threshold", "100",
		"--seats", "agent_safety_org,principal,deployer_compliance,model_provider_security,affected_node",
		"--charter-seats", "agent_safety_org,principal",
		"--charter-reason", "provider vacated",
		"--retain-segments", "2",
		"--retain-events=0",
		"--source", "incidents.jsonl",
		"demo", "-v",
	}
	o, pos, ok := parseArgs(args)
	if !ok {
		t.Fatal("parseArgs rejected a valid configuration surface")
	}
	if o.dataDir != "D:/ledger" || o.segmentSize != 8 || o.syncEveryAppend || o.fileMode != 0o640 {
		t.Fatalf("basic flags: %+v", o)
	}
	if o.clockSpec != "2020-01-01T00:00:00Z" || o.quota.CPUMillis != 250 || o.quota.MemoryBytes != 1<<30 || o.quota.StorageBytes != 2<<30 || o.quota.NetworkOpen {
		t.Fatalf("quota/clock flags: %+v", o)
	}
	if o.custody != 3 || o.monitor != 100 || len(o.seats) != 5 || len(o.charterSeats) != 2 || o.charterReason != "provider vacated" {
		t.Fatalf("governance flags: %+v", o)
	}
	if o.retainSegments != 2 || o.retainEvents != 0 || !o.retainEventsSet || o.sourcePath != "incidents.jsonl" || !o.verbose {
		t.Fatalf("retention/source flags: %+v", o)
	}
	if len(pos) != 1 || pos[0] != "demo" {
		t.Fatalf("positional = %v", pos)
	}
}

func TestParseArgsErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"segment zero", []string{"--segment-size", "0"}},
		{"segment bad", []string{"--segment-size", "x"}},
		{"sync bad", []string{"--sync-every-append=x"}},
		{"file mode bad", []string{"--file-mode", "0999"}},
		{"clock missing", []string{"--clock"}},
		{"quota cpu negative", []string{"--quota-cpu", "-1"}},
		{"quota memory bad", []string{"--quota-memory", "1X"}},
		{"quota network bad", []string{"--quota-network=maybe"}},
		{"custody one", []string{"--custody-m", "1"}},
		{"monitor zero", []string{"--monitor-threshold", "0"}},
		{"unknown seat", []string{"--seats", "nobody"}},
		{"charter missing", []string{"--charter-seats"}},
		{"retain segments negative", []string{"--retain-segments", "-1"}},
		{"retain events bad", []string{"--retain-events", "x"}},
		{"source missing", []string{"--source"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := captureCLI(t, func() int {
				if _, _, ok := parseArgs(tc.args); ok {
					return 0
				}
				return 2
			})
			if code != 2 {
				t.Fatalf("%s: parseArgs accepted invalid input", tc.name)
			}
		})
	}
}

func TestOptionsValidate(t *testing.T) {
	cases := []struct {
		name string
		opt  options
	}{
		{"bad clock", options{clockSpec: "yesterday"}},
		{"bad mode", options{fileMode: 0o1000, fileModeSet: true}},
		{"bad segment", options{segmentSize: -1}},
		{"custody one", options{custody: 1}},
		{"monitor above range", options{monitor: 1001}},
		{"missing seats", options{seats: []domain.CommitteeRole{domain.SeatSafetyOrg}}},
		{"charter without reason", options{charterSeats: []domain.CommitteeRole{domain.SeatSafetyOrg, domain.SeatPrincipal}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.opt.validate(); err == nil {
				t.Fatalf("%s: validate accepted invalid options", tc.name)
			}
		})
	}
}

func TestCLISurfaceOutput(t *testing.T) {
	if code, out, _ := captureCLI(t, func() int { return runCLI([]string{"version", "-v"}) }); code != 0 || !strings.Contains(out, "aiprison 1.0.9") || !strings.Contains(out, "commit:") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	if code, out, _ := captureCLI(t, func() int { return runCLI([]string{"levels"}) }); code != 0 || !strings.Contains(out, "L0") || !strings.Contains(out, "L5") {
		t.Fatalf("levels: code=%d out=%q", code, out)
	}
	if code, out, _ := captureCLI(t, func() int { return runCLI([]string{"config"}) }); code != 0 {
		t.Fatalf("config: code=%d", code)
	} else {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(out), &cfg); err != nil {
			t.Fatalf("config is not JSON: %v\n%s", err, out)
		}
		if cfg["segmentSize"] != float64(256) || cfg["charterSeats"] == nil {
			t.Fatalf("config defaults: %v", cfg)
		}
	}
	if code, out, _ := captureCLI(t, func() int { return runCLI([]string{"help"}) }); code != 0 || !strings.Contains(out, "usage: aiprison") {
		t.Fatalf("help: code=%d out=%q", code, out)
	}
	if code, out, _ := captureCLI(t, func() int { return runCLI(nil) }); code != 0 || !strings.Contains(out, "usage: aiprison") {
		t.Fatalf("no args: code=%d out=%q", code, out)
	}
	if code, _, errOut := captureCLI(t, func() int { return runCLI([]string{"no-such-command"}) }); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := captureCLI(t, func() int { return runCLI([]string{"--monitor-threshold", "0", "config"}) }); code != 2 || !strings.Contains(errOut, "invalid --monitor-threshold") {
		t.Fatalf("bad config: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := captureCLI(t, func() int { return runCLI([]string{"--bogus", "config"}) }); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown flag: code=%d err=%q", code, errOut)
	}
}

func TestDemoVerifyInspectAndRepair(t *testing.T) {
	dir := tempCLIDir(t, "demo")
	if code, out, errOut := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "demo"})
	}); code != 0 || !strings.Contains(out, "③基线") || !strings.Contains(out, "落盘摘要") {
		t.Fatalf("demo: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, _ := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "verify"})
	}); code != 0 || !strings.Contains(out, "INTEGRITY OK — 80 records") {
		t.Fatalf("verify: code=%d out=%q", code, out)
	}
	if code, out, _ := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "inspect"})
	}); code != 0 || !strings.Contains(out, "CHAIN VERIFY:    OK") || !strings.Contains(out, "数据目录") {
		t.Fatalf("inspect: code=%d out=%q", code, out)
	}

	segs, err := filepath.Glob(filepath.Join(dir, "seg-*.log"))
	if err != nil || len(segs) != 1 {
		t.Fatalf("segments: %v %v", segs, err)
	}
	f, err := os.OpenFile(segs[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := f.WriteString(`{"seq":81,"kind":"torn`); err != nil {
		t.Fatalf("append torn tail: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close segment: %v", err)
	}
	if _, err := store.OpenReadonly(dir); !store.IsTornTail(err) {
		t.Fatalf("test setup did not produce a torn tail: %v", err)
	}
	if code, out, errOut := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "repair-tail"})
	}); code != 0 || !strings.Contains(out, "已修复") || !strings.Contains(out, "INTEGRITY OK") {
		t.Fatalf("repair-tail: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, _ := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "verify"})
	}); code != 0 || !strings.Contains(out, "INTEGRITY OK — 80 records") {
		t.Fatalf("verify after repair: code=%d out=%q", code, out)
	}
}

func TestDrillCommand(t *testing.T) {
	dir := tempCLIDir(t, "drill")
	if code, out, errOut := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "drill"})
	}); code != 0 || !strings.Contains(out, "落盘摘要") || !strings.Contains(out, "PASS") {
		t.Fatalf("drill: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestWatchdogIngestAndMetrics(t *testing.T) {
	dir := tempCLIDir(t, "watchdog")
	src := filepath.Join(dir, "incidents.jsonl")
	lines := []string{
		`{"kind":"watchdog.incident","at":"2026-10-25T09:00:00Z","id":"inc-1"}`,
		`{"kind":"watchdog.incident","at":"2026-10-25T09:01:00Z","id":"inc-2"}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write incidents: %v", err)
	}
	code, out, errOut := captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "--source", src, "ingest-watchdog"})
	})
	if code != 0 {
		t.Fatalf("ingest: code=%d out=%q err=%q", code, out, errOut)
	}
	var ingest struct {
		Ingested int `json:"ingested"`
		Total    int `json:"ingestedTotal"`
	}
	if err := json.Unmarshal([]byte(out), &ingest); err != nil || ingest.Ingested != 2 || ingest.Total != 2 {
		t.Fatalf("ingest output: %v\n%s", err, out)
	}
	code, out, errOut = captureCLI(t, func() int {
		return runCLI([]string{"--data-dir", dir, "metrics"})
	})
	if code != 0 {
		t.Fatalf("metrics: code=%d out=%q err=%q", code, out, errOut)
	}
	var metrics struct {
		Records  int            `json:"records"`
		Kinds    map[string]int `json:"kinds"`
		Watchdog struct {
			Records int `json:"ledgerRecords"`
		} `json:"watchdog"`
	}
	if err := json.Unmarshal([]byte(out), &metrics); err != nil || metrics.Records != 2 || metrics.Watchdog.Records != 2 {
		t.Fatalf("metrics output: %v\n%s", err, out)
	}
}

func TestCLIProcessExitCodes(t *testing.T) {
	if os.Getenv("AIPRISON_CMD_HELPER") == "1" {
		dash := -1
		for i, a := range os.Args {
			if a == "--" {
				dash = i
				break
			}
		}
		if dash >= 0 {
			os.Exit(runCLI(os.Args[dash+1:]))
		}
		os.Exit(3)
	}

	base := tempCLIDir(t, "exit")
	notADir := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cases := []struct {
		name string
		args []string
		want int
		text string
	}{
		{"version", []string{"version"}, 0, "aiprison "},
		{"unknown command", []string{"no-such-command"}, 2, "unknown command"},
		{"bad flag", []string{"--monitor-threshold", "0", "config"}, 2, "invalid --monitor-threshold"},
		{"runtime failure", []string{"--data-dir", notADir, "demo"}, 1, "failed:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCLIProcessExitCodes$", "--")
			cmd.Args = append(cmd.Args, tc.args...)
			cmd.Env = append(os.Environ(), "AIPRISON_CMD_HELPER=1")
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run child: %v", err)
				}
				code = exitErr.ExitCode()
			}
			if code != tc.want || !strings.Contains(string(out), tc.text) {
				t.Fatalf("%s: code=%d want %d out=%q", tc.name, code, tc.want, out)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash("0123456789abcdef"); got != "0123456789ab" {
		t.Fatalf("shortHash long = %q", got)
	}
	if got := shortHash("short"); got != "short" {
		t.Fatalf("shortHash short = %q", got)
	}
}
