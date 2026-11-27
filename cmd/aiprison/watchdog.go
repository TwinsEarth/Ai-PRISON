// watchdog bridge: imports the DSH watchdog's append-only incident journal
// (incidents.jsonl) into the Go governance ledger as hash-chained
// `watchdog.incident` records, and prints the merged metrics view. The JS
// watchdog and the Go core used to share only a directory; this is the
// explicit ingest + metrics seam between them.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-prison/ai-prison/internal/store"
)

const watchdogIngestStateFile = "watchdog-ingest.json"

type ingestState struct {
	Source string `json:"source"`
	Offset int64  `json:"offset"`
	Count  int    `json:"count"`
	LastAt string `json:"lastAt"`
}

// incidentProbe validates the minimum shape the bridge relies on; the whole
// line is preserved verbatim as the ledger payload so no watchdog field is lost.
type incidentProbe struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

func ingestStatePath(dataDir string) string {
	return filepath.Join(dataDir, watchdogIngestStateFile)
}

func readIngestState(dataDir string) (ingestState, error) {
	var st ingestState
	raw, err := os.ReadFile(ingestStatePath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return ingestState{}, fmt.Errorf("watchdog ingest state %s: %w", ingestStatePath(dataDir), err)
	}
	return st, nil
}

// writeIngestState atomically replaces the state file so a crash can never
// persist a half-updated offset.
func writeIngestState(dataDir string, st ingestState) error {
	path := ingestStatePath(dataDir)
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// pendingIncidents reads the journal tail starting at the recorded byte
// offset. A truncated/rotated journal (size < offset) restarts from zero rather
// than skipping new incidents.
func pendingIncidents(source string, offset int64) (raw []byte, newOffset int64, err error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, offset, err
	}
	if info.Size() < offset {
		offset = 0
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	raw, err = io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	return raw, offset + int64(len(raw)), nil
}

// parseIncidents validates every non-empty line and returns the raw objects in
// order. A malformed line aborts the whole batch before anything is appended,
// so the ledger never holds half an import.
func parseIncidents(raw []byte) ([]json.RawMessage, error) {
	lines := bytes.Split(raw, []byte("\n"))
	out := make([]json.RawMessage, 0, len(lines))
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return nil, fmt.Errorf("line %d: not valid JSON", i+1)
		}
		var probe incidentProbe
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if strings.TrimSpace(probe.Kind) == "" {
			return nil, fmt.Errorf("line %d: missing kind", i+1)
		}
		out = append(out, json.RawMessage(append([]byte(nil), line...)))
	}
	return out, nil
}

func runIngestWatchdog(o options) {
	if o.dataDir == "" {
		fmt.Fprintln(os.Stderr, "ingest-watchdog: --data-dir is required")
		os.Exit(2)
	}
	if o.sourcePath == "" {
		fmt.Fprintln(os.Stderr, "ingest-watchdog: --source is required (path to watchdog incidents.jsonl)")
		os.Exit(2)
	}
	source, err := filepath.Abs(o.sourcePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: resolve --source: %v\n", err)
		os.Exit(1)
	}
	clk, err := o.resolveClock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %v\n", err)
		os.Exit(2)
	}
	so, err := o.storeOptions(clk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %v\n", err)
		os.Exit(2)
	}
	st, err := store.NewFileStore(o.dataDir, so)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	state, err := readIngestState(o.dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %v\n", err)
		os.Exit(1)
	}
	raw, next, err := pendingIncidents(source, state.Offset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: read %s: %v\n", source, err)
		os.Exit(1)
	}
	incidents, err := parseIncidents(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %s: %v\n", source, err)
		os.Exit(1)
	}

	ctx := context.Background()
	ingested := 0
	lastAt := state.LastAt
	for _, inc := range incidents {
		if _, err := st.Append(ctx, store.KindWatchdogIncident, inc); err != nil {
			fmt.Fprintf(os.Stderr, "ingest-watchdog: append: %v\n", err)
			os.Exit(1)
		}
		var probe incidentProbe
		_ = json.Unmarshal(inc, &probe)
		if probe.At != "" {
			lastAt = probe.At
		}
		ingested++
	}
	if err := st.Sync(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: sync: %v\n", err)
		os.Exit(1)
	}
	nextState := ingestState{Source: source, Offset: next, Count: state.Count + ingested, LastAt: lastAt}
	if err := writeIngestState(o.dataDir, nextState); err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: persist offset: %v\n", err)
		os.Exit(1)
	}

	stats := st.Stats()
	out := struct {
		OK       bool        `json:"ok"`
		Source   string      `json:"source"`
		Ingested int         `json:"ingested"`
		Total    int         `json:"ingestedTotal"`
		LastAt   string      `json:"lastIncidentAt,omitempty"`
		Offset   int64       `json:"offset"`
		Ledger   store.LedgerStats `json:"ledger"`
	}{true, source, ingested, nextState.Count, lastAt, next, stats}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "ingest-watchdog: %v\n", err)
		os.Exit(1)
	}
}

// runMetrics prints the merged ledger ∩ watchdog view: bounded ledger stats,
// record counts per kind, and how much of the watchdog journal is still pending.
func runMetrics(o options) {
	if o.dataDir == "" {
		fmt.Fprintln(os.Stderr, "metrics: --data-dir is required")
		os.Exit(2)
	}
	clk, err := o.resolveClock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics: %v\n", err)
		os.Exit(2)
	}
	so, err := o.storeOptions(clk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics: %v\n", err)
		os.Exit(2)
	}
	st, err := store.NewFileStore(o.dataDir, so)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	kinds := map[string]int{}
	if err := st.Replay(context.Background(), func(r store.Record) error {
		kinds[r.Kind]++
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "metrics: replay: %v\n", err)
		os.Exit(1)
	}
	stats := st.Stats()

	state, _ := readIngestState(o.dataDir)
	pending := int64(0)
	sourceSize := int64(0)
	sourcePath := o.sourcePath
	if sourcePath == "" {
		sourcePath = state.Source
	}
	if sourcePath != "" {
		if info, statErr := os.Stat(sourcePath); statErr == nil {
			sourceSize = info.Size()
			pending = sourceSize - state.Offset
			if pending < 0 {
				pending = sourceSize
			}
		}
	}

	out := struct {
		OK       bool              `json:"ok"`
		DataDir  string            `json:"dataDir"`
		Records  int               `json:"records"`
		Segments int               `json:"diskSegments"`
		Hot      map[string]any    `json:"hotWindow"`
		Kinds    map[string]int    `json:"kinds"`
		Watchdog map[string]any    `json:"watchdog"`
	}{
		OK:       true,
		DataDir:  o.dataDir,
		Records:  stats.TotalRecords,
		Segments: stats.DiskSegments,
		Hot: map[string]any{
			"baseSeq":        stats.BaseSeq,
			"inMemory":       stats.InMemory,
			"retainSegments": stats.RetainSegments,
			"segmentSize":    stats.SegmentSize,
		},
		Kinds: kinds,
		Watchdog: map[string]any{
			"source":       sourcePath,
			"offset":       state.Offset,
			"sourceSize":   sourceSize,
			"pendingBytes": pending,
			"ingested":     state.Count,
			"lastAt":       state.LastAt,
			"ledgerRecords": kinds[store.KindWatchdogIncident],
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "metrics: %v\n", err)
		os.Exit(1)
	}
}
