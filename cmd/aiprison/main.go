// Command aiprison is the CLI for the AI Prison governance prototype.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
	"github.com/ai-prison/ai-prison/internal/prison"
	"github.com/ai-prison/ai-prison/internal/store"
)

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

// runCLI is the process-independent command dispatcher. It returns the exit
// status instead of calling os.Exit so the cmd package can be tested in-process;
// fatal runtime paths still funnel through fail(), which exits 1.
func runCLI(argv []string) int {
	o, positional, ok := parseArgs(argv)
	if !ok {
		return 2
	}
	if len(positional) == 0 {
		printUsage()
		return 0
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	switch positional[0] {
	case "version":
		fmt.Println("aiprison", version)
		if o.verbose {
			// commit / buildTime default to "unknown" when not injected via
			// -ldflags -X; printing them anyway is intentional (no error).
			fmt.Printf("  commit:    %s\n", commit)
			fmt.Printf("  built:     %s\n", buildTime)
		}
	case "levels":
		printLevels()
	case "config":
		printConfig(o)
	case "demo":
		runDemo(o)
	case "drill":
		runDrill(o)
	case "inspect":
		runInspect(o.dataDir)
	case "verify":
		runVerify(o.dataDir)
	case "repair-tail":
		runRepairTail(o.dataDir)
	case "ingest-watchdog":
		runIngestWatchdog(o)
	case "metrics":
		runMetrics(o)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", positional[0])
		printUsage()
		return 2
	}
	return 0
}

// Release metadata. These are plain (not const) package variables so the build
// can override them at link time with
//
//	go build -ldflags "-X github.com/ai-prison/ai-prison/cmd/aiprison.version=$V ..."
//
// The defaults are what `go run` / `go build` without injection prints.
var (
	version   = "1.0.9"
	commit    = "unknown"
	buildTime = "unknown"
)

// options is the full user-facing configuration surface. Every field defaults
// to the value the previous release compiled in, so an empty options struct
// reproduces the 1.9.3 behaviour (and ledger) exactly.
type options struct {
	dataDir         string
	verbose         bool
	segmentSize     int
	syncEveryAppend bool
	fileMode        os.FileMode
	fileModeSet     bool
	clockSpec       string
	quota           prison.QuotaSpec
	quotaSet        bool
	custody         int
	monitor         int
	seats           []domain.CommitteeRole
	charterSeats    []domain.CommitteeRole
	charterReason   string
	retainSegments  int
	retainEvents    int
	retainEventsSet bool
	sourcePath      string
}

// resolveClock turns --clock into a clock function. Accepted values:
// "fixed" (the deterministic DefaultClock), "real" (time.Now) or an RFC3339
// instant that every event is stamped with.
func (o options) resolveClock() (func() time.Time, error) {
	spec := strings.TrimSpace(o.clockSpec)
	switch strings.ToLower(spec) {
	case "", "fixed":
		return prison.DefaultClock, nil
	case "real", "now":
		return time.Now, nil
	}
	t, err := time.Parse(time.RFC3339, spec)
	if err != nil {
		return nil, fmt.Errorf("--clock must be fixed|real|<RFC3339 instant>: %w", err)
	}
	fixed := t.UTC()
	return func() time.Time { return fixed }, nil
}

// storeOptions builds the ledger options from the resolved flags.
func (o options) storeOptions(clk func() time.Time) (store.Options, error) {
	mode := o.fileMode
	if !o.fileModeSet {
		mode = 0 // FileStore default 0600
	}
	if o.segmentSize < 0 {
		return store.Options{}, fmt.Errorf("--segment-size must be > 0")
	}
	return store.Options{
		SegmentSize:     o.segmentSize,
		SyncEveryAppend: o.syncEveryAppend,
		Clock:           clk,
		FileMode:        mode,
		RetainSegments:  o.retainSegments,
	}, nil
}

// retainEventsOrUnbounded maps the CLI convention (0 = unlimited) onto the
// prison Config convention (-1 = unbounded, 0 = default).
func (o options) retainEventsOrUnbounded() int {
	if o.retainEventsSet && o.retainEvents == 0 {
		return -1
	}
	return o.retainEvents
}

// prisonConfig builds the resolved orchestrator configuration.
func (o options) prisonConfig(clk func() time.Time, st store.Store) (prison.Config, error) {
	var quota *prison.QuotaSpec
	if o.quotaSet {
		q := o.quota
		quota = &q
	}
	cfg := prison.Config{
		Clock:            clk,
		Store:            st,
		Quota:            quota,
		CustodyThreshold: o.custody,
		MonitorThreshold: o.monitor,
		Seats:            o.seats,
		CharterSeats:     o.charterSeats,
		CharterReason:    o.charterReason,
		RetainEvents:     o.retainEventsOrUnbounded(),
	}
	if err := cfg.Validate(); err != nil {
		return prison.Config{}, err
	}
	return cfg, nil
}

func (o options) validate() error {
	if _, err := o.resolveClock(); err != nil {
		return err
	}
	if o.fileModeSet && o.fileMode > 0o777 {
		return fmt.Errorf("--file-mode must be between 0000 and 0777, got %#o", o.fileMode)
	}
	if _, err := o.storeOptions(prison.DefaultClock); err != nil {
		return err
	}
	if _, err := o.prisonConfig(prison.DefaultClock, nil); err != nil {
		return err
	}
	return nil
}

// parseArgs splits argv into global flags (accepted before OR after the
// subcommand) and the remaining positional arguments. ok=false means a usage
// error has already been printed.
func parseArgs(argv []string) (options, []string, bool) {
	o := options{retainSegments: store.DefaultRetainSegments}
	var positional []string
	fail := func(format string, args ...any) (options, []string, bool) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		return options{}, nil, false
	}
	// Environment first, flags second: the DSH plugin spawns this CLI with only
	// --data-dir, so AIPRISON_* variables are how an operator configures it
	// there. Anything given on the command line still wins.
	if err := o.applyEnv(os.Getenv); err != nil {
		return fail("%v", err)
	}
	// take returns the flag's value: either the inline `--k=v` part or the next
	// argv element.
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, inline, hasInline := splitFlag(arg)
		value := func() (string, bool) {
			if hasInline {
				return inline, true
			}
			if i+1 < len(argv) {
				i++
				return argv[i], true
			}
			return "", false
		}
		switch name {
		case "--data-dir":
			v, ok := value()
			if !ok {
				return fail("flag --data-dir requires a path argument")
			}
			o.dataDir = v
		case "-v", "--verbose":
			o.verbose = true
		case "--segment-size":
			v, ok := value()
			if !ok {
				return fail("flag --segment-size requires a number")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fail("invalid --segment-size %q: must be a positive integer", v)
			}
			o.segmentSize = n
		case "--sync-every-append":
			o.syncEveryAppend = true
			if hasInline {
				b, err := strconv.ParseBool(inline)
				if err != nil {
					return fail("invalid --sync-every-append %q: want true|false", inline)
				}
				o.syncEveryAppend = b
			}
		case "--file-mode":
			v, ok := value()
			if !ok {
				return fail("flag --file-mode requires an octal mode (e.g. 0600)")
			}
			n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(v, "0o"), "0O"), 8, 32)
			if err != nil {
				return fail("invalid --file-mode %q: want octal, e.g. 0640", v)
			}
			o.fileMode = os.FileMode(n)
			o.fileModeSet = true
		case "--clock":
			v, ok := value()
			if !ok {
				return fail("flag --clock requires fixed|real|<RFC3339 instant>")
			}
			o.clockSpec = v
		case "--quota-cpu":
			v, ok := value()
			if !ok {
				return fail("flag --quota-cpu requires milliseconds")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fail("invalid --quota-cpu %q: must be a non-negative integer (millicores)", v)
			}
			o.ensureQuota()
			o.quota.CPUMillis = n
		case "--quota-memory":
			v, ok := value()
			if !ok {
				return fail("flag --quota-memory requires a byte size (e.g. 4G)")
			}
			n, err := parseSize(v)
			if err != nil {
				return fail("invalid --quota-memory %q: %v", v, err)
			}
			o.ensureQuota()
			o.quota.MemoryBytes = n
		case "--quota-storage":
			v, ok := value()
			if !ok {
				return fail("flag --quota-storage requires a byte size (e.g. 10G)")
			}
			n, err := parseSize(v)
			if err != nil {
				return fail("invalid --quota-storage %q: %v", v, err)
			}
			o.ensureQuota()
			o.quota.StorageBytes = n
		case "--quota-network":
			raw := "true"
			if hasInline {
				raw = inline
			}
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return fail("invalid --quota-network %q: want true|false", raw)
			}
			o.ensureQuota()
			o.quota.NetworkOpen = b
		case "--custody-m":
			v, ok := value()
			if !ok {
				return fail("flag --custody-m requires 2 or 3")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 2 || n > 3 {
				return fail("invalid --custody-m %q: want 2 or 3 (M-of-3; 1 would be single-party custody)", v)
			}
			o.custody = n
		case "--monitor-threshold":
			v, ok := value()
			if !ok {
				return fail("flag --monitor-threshold requires an integer in 1..1000")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1000 {
				return fail("invalid --monitor-threshold %q: want 1..1000 (0 would freeze on the first signal)", v)
			}
			o.monitor = n
		case "--seats":
			v, ok := value()
			if !ok {
				return fail("flag --seats requires a comma-separated roster")
			}
			seats, err := parseSeats(v)
			if err != nil {
				return fail("invalid --seats %q: %v", v, err)
			}
			o.seats = seats
		case "--charter-seats":
			v, ok := value()
			if !ok {
				return fail("flag --charter-seats requires a comma-separated subset of the four charter seats")
			}
			seats, err := parseSeats(v)
			if err != nil {
				return fail("invalid --charter-seats %q: %v", v, err)
			}
			o.charterSeats = seats
		case "--charter-reason":
			v, ok := value()
			if !ok {
				return fail("flag --charter-reason requires a non-empty explanation")
			}
			o.charterReason = v
		case "--retain-segments":
			v, ok := value()
			if !ok {
				return fail("flag --retain-segments requires a non-negative integer (0 = keep all)")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fail("invalid --retain-segments %q: want >= 0 (0 = keep every segment resident)", v)
			}
			o.retainSegments = n
		case "--retain-events":
			v, ok := value()
			if !ok {
				return fail("flag --retain-events requires a non-negative integer (0 = keep all)")
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fail("invalid --retain-events %q: want >= 0 (0 = keep every event resident)", v)
			}
			o.retainEvents = n
			o.retainEventsSet = true
		case "--source":
			v, ok := value()
			if !ok {
				return fail("flag --source requires a path (watchdog incidents.jsonl)")
			}
			o.sourcePath = v
		default:
			positional = append(positional, arg)
		}
	}
	return o, positional, true
}

// applyEnv seeds the options from AIPRISON_* variables. Without them nothing
// changes, so an unconfigured environment keeps the 1.9.3 defaults.
func (o *options) applyEnv(getenv func(string) string) error {
	env := func(k string) string { return strings.TrimSpace(getenv(k)) }

	if v := env("AIPRISON_SEGMENT_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("AIPRISON_SEGMENT_SIZE=%q: must be a positive integer", v)
		}
		o.segmentSize = n
	}
	if v := env("AIPRISON_SYNC_EVERY_APPEND"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_SYNC_EVERY_APPEND=%q: want true|false", v)
		}
		o.syncEveryAppend = b
	}
	if v := env("AIPRISON_FILE_MODE"); v != "" {
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(v, "0o"), "0O"), 8, 32)
		if err != nil {
			return fmt.Errorf("AIPRISON_FILE_MODE=%q: want octal, e.g. 0640", v)
		}
		o.fileMode = os.FileMode(n)
		o.fileModeSet = true
	}
	if v := env("AIPRISON_CLOCK"); v != "" {
		o.clockSpec = v
	}
	if v := env("AIPRISON_QUOTA_CPU"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("AIPRISON_QUOTA_CPU=%q: must be a non-negative integer (millicores)", v)
		}
		o.ensureQuota()
		o.quota.CPUMillis = n
	}
	if v := env("AIPRISON_QUOTA_MEMORY"); v != "" {
		n, err := parseSize(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_QUOTA_MEMORY=%q: %v", v, err)
		}
		o.ensureQuota()
		o.quota.MemoryBytes = n
	}
	if v := env("AIPRISON_QUOTA_STORAGE"); v != "" {
		n, err := parseSize(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_QUOTA_STORAGE=%q: %v", v, err)
		}
		o.ensureQuota()
		o.quota.StorageBytes = n
	}
	if v := env("AIPRISON_QUOTA_NETWORK"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_QUOTA_NETWORK=%q: want true|false", v)
		}
		o.ensureQuota()
		o.quota.NetworkOpen = b
	}
	if v := env("AIPRISON_CUSTODY_M"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 || n > 3 {
			return fmt.Errorf("AIPRISON_CUSTODY_M=%q: want 2 or 3 (M-of-3)", v)
		}
		o.custody = n
	}
	if v := env("AIPRISON_MONITOR_THRESHOLD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			return fmt.Errorf("AIPRISON_MONITOR_THRESHOLD=%q: want 1..1000", v)
		}
		o.monitor = n
	}
	if v := env("AIPRISON_SEATS"); v != "" {
		seats, err := parseSeats(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_SEATS=%q: %v", v, err)
		}
		o.seats = seats
	}
	if v := env("AIPRISON_CHARTER_SEATS"); v != "" {
		seats, err := parseSeats(v)
		if err != nil {
			return fmt.Errorf("AIPRISON_CHARTER_SEATS=%q: %v", v, err)
		}
		o.charterSeats = seats
	}
	if v := env("AIPRISON_CHARTER_REASON"); v != "" {
		o.charterReason = v
	}
	if v := env("AIPRISON_RETAIN_SEGMENTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("AIPRISON_RETAIN_SEGMENTS=%q: want >= 0 (0 = keep all)", v)
		}
		o.retainSegments = n
	}
	if v := env("AIPRISON_RETAIN_EVENTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("AIPRISON_RETAIN_EVENTS=%q: want >= 0 (0 = keep all)", v)
		}
		o.retainEvents = n
		o.retainEventsSet = true
	}
	if v := env("AIPRISON_WATCHDOG_INCIDENTS"); v != "" {
		o.sourcePath = v
	}
	return nil
}

// ensureQuota fills the quota with defaults the first time any --quota-* flag
// is seen, so a single flag can be changed without restating the rest.
func (o *options) ensureQuota() {
	if o.quotaSet {
		return
	}
	o.quota = prison.DefaultQuotaSpec()
	o.quotaSet = true
}

// splitFlag splits `--name=value` into its name and inline value.
func splitFlag(arg string) (name, value string, hasValue bool) {
	if i := strings.IndexByte(arg, '='); i > 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

// parseSize accepts a plain byte count or a K/M/G/T suffix (binary multiples).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "T"):
		mult, s = 1<<40, strings.TrimSuffix(s, "T")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("want a positive number, optionally with a K/M/G/T suffix")
	}
	return n * mult, nil
}

// parseSeats splits a comma-separated roster and rejects unknown roles.
func parseSeats(raw string) ([]domain.CommitteeRole, error) {
	known := map[string]domain.CommitteeRole{}
	for _, s := range prison.AllSeats() {
		known[string(s)] = s
	}
	parts := strings.Split(raw, ",")
	out := make([]domain.CommitteeRole, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		role, ok := known[p]
		if !ok {
			return nil, fmt.Errorf("unknown seat %q (known: %s)", p, strings.Join(roleNames(prison.AllSeats()), ", "))
		}
		out = append(out, role)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty roster")
	}
	return out, nil
}

func roleNames(roles []domain.CommitteeRole) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, string(r))
	}
	return out
}

// effectiveConfig is the printable form of `aiprison config`.
type effectiveConfig struct {
	DataDir          string                 `json:"dataDir"`
	SegmentSize      int                    `json:"segmentSize"`
	SyncEveryAppend  bool                   `json:"syncEveryAppend"`
	FileMode         string                 `json:"fileMode"`
	Clock            string                 `json:"clock"`
	Quota            prison.QuotaSpec       `json:"quota"`
	CustodyThreshold int                    `json:"custodyThreshold"`
	MonitorThreshold int                    `json:"monitorThreshold"`
	Seats            []domain.CommitteeRole `json:"seats"`
	CharterSeats     []domain.CommitteeRole `json:"charterSeats"`
	CharterReason    string                 `json:"charterReason,omitempty"`
	RetainSegments   int                    `json:"retainSegments"`
	RetainEvents     int                    `json:"retainEvents"`
}

// printConfig prints the effective configuration as JSON, with every default
// resolved. It never touches the data directory.
func printConfig(o options) {
	clk, err := o.resolveClock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	st, err := o.storeOptions(clk)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	cfg, err := o.prisonConfig(clk, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	cfg = cfg.Resolve()

	segmentSize := st.SegmentSize
	if segmentSize <= 0 {
		segmentSize = 256
	}
	mode := st.FileMode
	if mode == 0 {
		mode = 0o600
	}
	clockDesc := "fixed@2026-10-25T09:00:00Z"
	if strings.EqualFold(strings.TrimSpace(o.clockSpec), "real") || strings.EqualFold(strings.TrimSpace(o.clockSpec), "now") {
		clockDesc = "real"
	} else if spec := strings.TrimSpace(o.clockSpec); spec != "" && !strings.EqualFold(spec, "fixed") {
		clockDesc = "fixed@" + spec
	}

	out := effectiveConfig{
		DataDir:          o.dataDir,
		SegmentSize:      segmentSize,
		SyncEveryAppend:  st.SyncEveryAppend,
		FileMode:         fmt.Sprintf("%#o", mode),
		Clock:            clockDesc,
		Quota:            *cfg.Quota,
		CustodyThreshold: cfg.CustodyThreshold,
		MonitorThreshold: cfg.MonitorThreshold,
		Seats:            cfg.Seats,
		CharterSeats:     cfg.CharterSeats,
		CharterReason:    cfg.CharterReason,
		RetainSegments:   st.RetainSegments,
		RetainEvents:     cfg.RetainEvents,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("aiprison — defensive AI-agent governance prototype")
	fmt.Println()
	fmt.Println("usage: aiprison [flags] <command> [flags]")
	fmt.Println()
	fmt.Println("global flags:")
	fmt.Println("  --data-dir DIR        durable governance ledger directory (default: pure in-memory)")
	fmt.Println("  --segment-size N      records buffered per segment before publish (default 256)")
	fmt.Println("  --sync-every-append   fsync after every append (default: batched, Close/Sync is the boundary)")
	fmt.Println("  --file-mode OCT       segment file mode (default 0600)")
	fmt.Println("  --clock SPEC          fixed (deterministic demo clock, default) | real | <RFC3339 instant>")
	fmt.Println("  --quota-cpu N         base CPU quota in millicores (default 1000)")
	fmt.Println("  --quota-memory SIZE   base memory quota, e.g. 4G (default 4G)")
	fmt.Println("  --quota-storage SIZE  base storage quota, e.g. 10G (default 10G)")
	fmt.Println("  --quota-network BOOL   base network access (default true)")
	fmt.Println("  --custody-m N         snapshot custody threshold, 2..3 (default 2)")
	fmt.Println("  --monitor-threshold N behaviour-monitor freeze threshold, 1..1000 (default 10)")
	fmt.Println("  --seats LIST          committee roster; the four charter seats are mandatory:")
	fmt.Println("                        agent_safety_org,principal,deployer_compliance,model_provider_security[,...]")
	fmt.Println("  --charter-seats LIST  amend the mandatory set: a subset (>=2) of the four charter seats;")
	fmt.Println("                        removing a seat also requires --charter-reason")
	fmt.Println("  --charter-reason TEXT reason recorded when a charter seat is removed")
	fmt.Println("  --retain-segments N   hot segments kept in memory (default 8); 0 keeps the whole ledger resident")
	fmt.Println("  --retain-events N     audit-chain events kept fully resident (default 1024); 0 keeps all")
	fmt.Println("  --source PATH         watchdog incidents.jsonl for ingest-watchdog / metrics")
	fmt.Println("  -v, --verbose         print injected commit/build time (version only)")
	fmt.Println()
	fmt.Println("environment defaults (flags above win; the DSH plugin uses these):")
	fmt.Println("  AIPRISON_SEGMENT_SIZE, AIPRISON_SYNC_EVERY_APPEND, AIPRISON_FILE_MODE,")
	fmt.Println("  AIPRISON_CLOCK, AIPRISON_QUOTA_CPU, AIPRISON_QUOTA_MEMORY,")
	fmt.Println("  AIPRISON_QUOTA_STORAGE, AIPRISON_QUOTA_NETWORK, AIPRISON_CUSTODY_M,")
	fmt.Println("  AIPRISON_MONITOR_THRESHOLD, AIPRISON_SEATS, AIPRISON_CHARTER_SEATS,")
	fmt.Println("  AIPRISON_CHARTER_REASON, AIPRISON_RETAIN_SEGMENTS,")
	fmt.Println("  AIPRISON_RETAIN_EVENTS")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  version [-v|--verbose]  print the version (verbose adds injected commit/build time)")
	fmt.Println("  config              print the effective configuration as JSON (no data dir is touched)")
	fmt.Println("  levels              print the L0–L5 grading matrix")
	fmt.Println("  demo                run the end-to-end demo (persists when --data-dir is set)")
	fmt.Println("  drill               run the recovery drill (persists when --data-dir is set)")
	fmt.Println("  inspect             read-only replay of --data-dir: chain, agents, sentences, revocations, receipts, snapshots")
	fmt.Println("  verify              integrity-check --data-dir; INTEGRITY OK + exit 0, else locate tamper/torn tail + exit 1")
	fmt.Println("  repair-tail         remove only a genuine torn (incomplete) tail line; refuse hard tamper")
	fmt.Println("  ingest-watchdog     append watchdog incidents.jsonl lines to the ledger as watchdog.incident records (incremental, atomic offset)")
	fmt.Println("  metrics             merged JSON view: ledger records/segments/hot window/kinds + watchdog ingest offset and pending bytes")
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
