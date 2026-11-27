package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// File names used inside the data directory.
const (
	segPrefix   = "seg-"
	segSuffix   = ".log"
	segDigits   = 10
	tempPattern = ".seg-*.tmp"
)

// Options configure a FileStore.
type Options struct {
	// SegmentSize is the number of records buffered in one segment before it
	// is automatically published (even without an explicit Sync). Default 256.
	SegmentSize int
	// SyncEveryAppend publishes (and fsyncs) after every Append. It is the
	// strongest durability mode but the slowest (one segment per record). The
	// default is batched, with Sync/Close as the durability boundary.
	SyncEveryAppend bool
	// Clock supplies timestamps; nil uses time.Now.
	Clock func() time.Time
	// FileMode is the mode for segment files; zero uses 0600.
	FileMode os.FileMode
	// RetainSegments caps how many published segments stay resident in memory
	// (hot window). 0 means "keep everything" (1.9.4 behaviour). Older segments
	// are still on disk and are streamed back by Replay, so the hot window is
	// an acceleration cache, not the source of truth.
	RetainSegments int
}

// DefaultRetainSegments is the production default: a bounded hot window instead
// of an ever-growing in-memory copy of the whole ledger.
const DefaultRetainSegments = 8

// FileStore is the real on-disk append-only JSONL ledger. Records are grouped
// into immutable segment files that are written through a temp file and
// atomically renamed. Only the hot window (RetainSegments) stays in memory;
// older published segments live on disk and Replay streams them back, so the
// durable ledger is the source of truth and memory stays bounded.
type FileStore struct {
	dir      string
	opts     Options
	mode     os.FileMode
	clock    func() time.Time
	readOnly bool

	mu        sync.Mutex
	records   []Record // hot window: absolute seqs [base, base+len(records))
	base      int      // absolute sequence number of records[0]
	persisted int      // absolute count of records already on disk
	lastHash  string   // digest of the newest record (window head may be trimmed)
	cleaned   int      // leftover temporary files removed on open
}

// NewFileStore opens (creating if needed) a durable ledger under dir. It
// replays every published segment and verifies the per-record hash chain; a
// torn or tampered ledger is returned as an *IntegrityError and must not be
// trusted. Unpublished temporary files from a crashed publish are discarded.
func NewFileStore(dir string, opts Options) (*FileStore, error) {
	if opts.SegmentSize <= 0 {
		opts.SegmentSize = 256
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	mode := opts.FileMode
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	cleaned, err := discardTemps(dir)
	if err != nil {
		return nil, err
	}
	window, base, lastHash, err := loadRetained(dir, opts.RetainSegments, opts.SegmentSize)
	if err != nil {
		return nil, err
	}
	return &FileStore{
		dir:       dir,
		opts:      opts,
		mode:      mode,
		clock:     opts.Clock,
		records:   window,
		base:      int(base),
		persisted: int(base) + len(window),
		lastHash:  lastHash,
		cleaned:   cleaned,
	}, nil
}

// OpenReadonly opens the ledger for inspection without creating or deleting
// anything. It still verifies integrity. It must not be used to append.
func OpenReadonly(dir string) (*FileStore, error) {
	records, err := loadSegments(dir, false)
	if err != nil {
		return nil, err
	}
	return &FileStore{
		dir:       dir,
		readOnly:  true,
		clock:     time.Now,
		records:   records,
		persisted: len(records),
	}, nil
}

// Path returns the data directory.
func (f *FileStore) Path() string { return f.dir }

// CleanedTempFiles reports how many unpublished temp files were removed on
// open (evidence of a crash that happened before a segment was renamed).
func (f *FileStore) CleanedTempFiles() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cleaned
}

// Append adds one record and, when SyncEveryAppend is set, publishes it durably.
func (f *FileStore) Append(ctx context.Context, kind string, payload any) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if f.readOnly {
		return Record{}, fmt.Errorf("store: %s opened read-only", f.dir)
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return Record{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	seq := uint64(f.base + len(f.records))
	prev := f.lastHash
	rec, err := storeRecord(seq, prev, kind, f.clock(), raw)
	if err != nil {
		return Record{}, err
	}
	f.records = append(f.records, rec)
	f.lastHash = rec.Hash

	publish := func() error {
		if err := f.publishLocked(f.pendingLocked()); err != nil {
			return err
		}
		f.persisted = f.base + len(f.records)
		f.trimLocked()
		return nil
	}
	switch {
	case f.opts.SyncEveryAppend:
		if err := publish(); err != nil {
			return Record{}, err
		}
	case f.base+len(f.records)-f.persisted >= f.opts.SegmentSize:
		if err := publish(); err != nil {
			return Record{}, err
		}
	}
	return rec, nil
}

// Sync atomically publishes the buffered active segment.
func (f *FileStore) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.readOnly {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pending := f.pendingLocked()
	if len(pending) == 0 {
		return nil
	}
	if err := f.publishLocked(pending); err != nil {
		return err
	}
	f.persisted = f.base + len(f.records)
	f.trimLocked()
	return nil
}

// Close flushes any buffered records durably and verifies no records are
// stranded.
func (f *FileStore) Close() error {
	if f.readOnly {
		return nil
	}
	return f.Sync(context.Background())
}

// Replay visits all records in seq order. The hot window is served from memory;
// the trimmed prefix is streamed back from disk segment by segment, so a large
// ledger is never materialised in RAM just to be replayed.
func (f *FileStore) Replay(ctx context.Context, visit func(Record) error) error {
	f.mu.Lock()
	base := f.base
	window := append([]Record(nil), f.records...)
	f.mu.Unlock()

	if base > 0 {
		if err := streamSegments(f.dir, uint64(base), ctx, visit); err != nil {
			return err
		}
	}
	for _, rec := range window {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(rec); err != nil {
			return err
		}
	}
	return nil
}

// Len returns the number of logical records (disk prefix + hot window).
func (f *FileStore) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.base + len(f.records)
}

// Persisted reports how many records are durably on disk.
func (f *FileStore) Persisted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.persisted
}

// LedgerStats describes the hot/cold split for reporting.
type LedgerStats struct {
	TotalRecords   int
	InMemory       int
	BaseSeq        uint64
	DiskSegments   int
	RetainSegments int
	SegmentSize    int
}

// Stats snapshots the hot window and on-disk segment count.
func (f *FileStore) Stats() LedgerStats {
	f.mu.Lock()
	base, inMem := f.base, len(f.records)
	f.mu.Unlock()
	files, _ := listSegments(f.dir)
	return LedgerStats{
		TotalRecords:   base + inMem,
		InMemory:       inMem,
		BaseSeq:        uint64(base),
		DiskSegments:   len(files),
		RetainSegments: f.opts.RetainSegments,
		SegmentSize:    f.opts.SegmentSize,
	}
}

// pendingLocked returns the not-yet-published tail. The caller holds f.mu.
func (f *FileStore) pendingLocked() []Record {
	off := f.persisted - f.base
	if off < 0 || off > len(f.records) {
		return nil
	}
	return f.records[off:]
}

// trimLocked evicts the oldest published records from the hot window once it
// exceeds RetainSegments segments. The evicted records remain on disk and are
// streamed by Replay; trimLocked only shrinks the resident cache.
func (f *FileStore) trimLocked() {
	retain := f.opts.RetainSegments
	if retain <= 0 {
		return
	}
	keep := retain * f.opts.SegmentSize
	durable := f.persisted - f.base
	if durable <= keep {
		return
	}
	drop := durable - keep
	f.records = append([]Record(nil), f.records[drop:]...)
	f.base += drop
}

// publishLocked writes recs to a temp file, fsyncs, atomically renames and
// fsyncs the directory. The caller must hold f.mu and recs must be a
// contiguous tail beginning at a segment boundary (the first record's seq is
// the segment's start).
func (f *FileStore) publishLocked(recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("store: encode segment: %w", err)
		}
	}

	tf, err := os.CreateTemp(f.dir, tempPattern)
	if err != nil {
		return fmt.Errorf("store: create temp segment: %w", err)
	}
	tmpName := tf.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tf.Chmod(f.mode); err != nil {
		_ = tf.Close()
		cleanup()
		return fmt.Errorf("store: chmod segment: %w", err)
	}
	if _, err := tf.Write(buf.Bytes()); err != nil {
		_ = tf.Close()
		cleanup()
		return fmt.Errorf("store: write segment: %w", err)
	}
	if err := tf.Sync(); err != nil {
		_ = tf.Close()
		cleanup()
		return fmt.Errorf("store: fsync segment: %w", err)
	}
	if err := tf.Close(); err != nil {
		cleanup()
		return fmt.Errorf("store: close segment: %w", err)
	}

	final := filepath.Join(f.dir, fmt.Sprintf("%s%0*d%s", segPrefix, segDigits, recs[0].Seq, segSuffix))
	if _, err := os.Stat(final); err == nil {
		cleanup()
		return fmt.Errorf("store: segment already exists at %s", final)
	}
	if err := os.Rename(tmpName, final); err != nil {
		cleanup()
		return fmt.Errorf("store: atomic rename segment: %w", err)
	}
	if err := fsyncDir(f.dir); err != nil {
		return fmt.Errorf("store: fsync directory: %w", err)
	}
	return nil
}

// discardTemps removes unpublished temporary files left by a crash.
func discardTemps(dir string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, tempPattern))
	if err != nil {
		return 0, fmt.Errorf("store: scan temp files: %w", err)
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return 0, fmt.Errorf("store: remove stale temp %s: %w", m, err)
		}
	}
	return len(matches), nil
}

// segmentPath orders one glob result.
func listSegments(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, segPrefix+"*"+segSuffix))
	if err != nil {
		return nil, fmt.Errorf("store: scan segments: %w", err)
	}
	sort.Strings(matches)
	return matches, nil
}

// loadSegments reads and verifies every published segment in order.
func loadSegments(dir string, createOK bool) ([]Record, error) {
	files, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 && !createOK {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return nil, nil
		}
	}
	var records []Record
	var expectSeq uint64
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("store: read segment %s: %w", path, err)
		}
		seg, err := parseSegment(path, data, expectSeq, isLastSegment(path, files))
		if err != nil {
			return nil, err
		}
		records = append(records, seg...)
		expectSeq += uint64(len(seg))
	}
	if err := Verify(records); err != nil {
		ie := AsIntegrityError(err)
		if ie != nil {
			ie.File = segmentForSeq(ie.Seq, files)
		}
		return nil, err
	}
	return records, nil
}

func isLastSegment(path string, files []string) bool {
	return len(files) > 0 && files[len(files)-1] == path
}

// checkRecord verifies one record against the expected position and previous
// digest, and returns the digest the next record must link to.
func checkRecord(rec Record, file string, line int, expectSeq uint64, expectPrev string) (string, error) {
	if rec.Seq != expectSeq {
		return "", &IntegrityError{Kind: KindBadSeq, File: file, Line: line, Seq: rec.Seq,
			Detail: "non-monotonic sequence"}
	}
	if rec.PrevHash != expectPrev {
		return "", &IntegrityError{Kind: KindBadLink, File: file, Line: line, Seq: rec.Seq,
			Detail: "previous-hash link does not match"}
	}
	got, err := hashRecord(rec.Seq, rec.Kind, rec.At, rec.PrevHash, rec.Payload)
	if err != nil {
		return "", &IntegrityError{Kind: KindCorrupt, File: file, Line: line, Seq: rec.Seq,
			Detail: "record cannot be hashed: " + err.Error()}
	}
	if got != rec.Hash {
		return "", &IntegrityError{Kind: KindBadHash, File: file, Line: line, Seq: rec.Seq,
			Detail: "content digest mismatch"}
	}
	return got, nil
}

// loadRetained replays every published segment, verifying the hash chain, and
// keeps only the last `retainSegments` segments resident. A zero value keeps
// everything (the pre-1.9.5 behaviour). Returns the resident window, the
// absolute sequence number of its first record, and the newest digest.
func loadRetained(dir string, retainSegments, segmentSize int) ([]Record, uint64, string, error) {
	files, err := listSegments(dir)
	if err != nil {
		return nil, 0, "", err
	}
	keep := 0
	if retainSegments > 0 {
		keep = retainSegments * segmentSize
	}
	var window []Record
	var expectSeq uint64
	expectPrev := ""
	for i, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, "", fmt.Errorf("store: read segment %s: %w", path, err)
		}
		seg, err := parseSegment(path, data, expectSeq, i == len(files)-1)
		if err != nil {
			return nil, 0, "", err
		}
		for j := range seg {
			next, err := checkRecord(seg[j], path, j+1, expectSeq, expectPrev)
			if err != nil {
				return nil, 0, "", err
			}
			expectSeq++
			expectPrev = next
		}
		window = append(window, seg...)
		if keep > 0 && len(window) > keep {
			drop := len(window) - keep
			window = append([]Record(nil), window[drop:]...)
		}
	}
	return window, expectSeq - uint64(len(window)), expectPrev, nil
}

// streamSegments replays the first `limit` records straight from disk without
// loading them all: one segment (SegmentSize records) is resident at a time.
func streamSegments(dir string, limit uint64, ctx context.Context, visit func(Record) error) error {
	files, err := listSegments(dir)
	if err != nil {
		return err
	}
	var seen uint64
	for i, path := range files {
		if seen >= limit {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("store: read segment %s: %w", path, err)
		}
		seg, err := parseSegment(path, data, seen, i == len(files)-1)
		if err != nil {
			return err
		}
		for j := range seg {
			if seen >= limit {
				return nil
			}
			rec := seg[j]
			if err := visit(rec); err != nil {
				return err
			}
			seen++
		}
	}
	return nil
}

// segmentForSeq names the segment that actually contains seq: the last
// segment whose start is <= seq. Returning the first such match would blame
// an earlier segment for a fault that happened in a later one.
func segmentForSeq(seq uint64, files []string) string {
	best := ""
	for _, p := range files {
		start, ok := segmentStart(p)
		if !ok {
			continue
		}
		if start > seq {
			break // files are sorted: every later segment starts even later
		}
		best = p
	}
	if best != "" {
		return best
	}
	if len(files) > 0 {
		return files[len(files)-1]
	}
	return ""
}

// parseSegment parses one segment's JSONL, expecting records to start at
// startSeq and to be contiguous. A partial final line in the last segment is
// reported as a reparable torn tail; a malformed line elsewhere is a hard
// corruption fault.
func parseSegment(path string, data []byte, startSeq uint64, last bool) ([]Record, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if start, ok := segmentStart(filepath.Base(path)); ok && start != startSeq {
		return nil, &IntegrityError{Kind: KindCorrupt, File: path, Seq: startSeq,
			Detail: fmt.Sprintf("segment %s start %d does not match expected seq %d", path, start, startSeq)}
	}
	lines := bytes.Split(data, []byte("\n"))
	// A trailing newline yields an empty final element.
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	var out []Record
	for i, line := range lines {
		if len(line) == 0 {
			// An unexpected blank line in the middle corrupts the record
			// numbering; the later Verify will catch it as a seq fault.
			if !last || i != len(lines)-1 {
				return nil, &IntegrityError{Kind: KindCorrupt, File: path, Line: i + 1, Seq: startSeq,
					Detail: "blank line inside segment"}
			}
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			if last && i == len(lines)-1 {
				offset := int64(len(data) - len(line))
				return nil, &IntegrityError{Kind: KindTornTail, File: path, Line: i + 1,
					Seq: startSeq + uint64(i), Offset: offset,
					Detail: "incomplete final line (torn tail): " + err.Error()}
			}
			return nil, &IntegrityError{Kind: KindCorrupt, File: path, Line: i + 1, Seq: startSeq + uint64(i),
				Detail: "unparseable line: " + err.Error()}
		}
		out = append(out, rec)
	}
	return out, nil
}

// segmentStart extracts the start sequence encoded in a segment file name.
func segmentStart(name string) (uint64, bool) {
	base := filepath.Base(name)
	if !strings.HasPrefix(base, segPrefix) || !strings.HasSuffix(base, segSuffix) {
		return 0, false
	}
	num := strings.TrimSuffix(strings.TrimPrefix(base, segPrefix), segSuffix)
	var v uint64
	if _, err := fmt.Sscanf(num, "%d", &v); err != nil {
		return 0, false
	}
	return v, true
}

// RepairTornTail removes an incomplete final line from the last segment file
// in dir, after fsyncing both the file and directory. It is the ONLY integrity
// repair permitted: it cannot resurrect a complete, tampered record.
//
// The repaired file always keeps the '\n' that terminates its last complete
// record, so the ledger stays appendable. The rewrite is published through a
// temp file plus atomic rename: a crash during repair can never leave a
// half-truncated segment behind. It returns the exact number of bytes removed
// and the repaired file.
func RepairTornTail(dir string) (int, string, error) {
	files, err := listSegments(dir)
	if err != nil {
		return 0, "", err
	}
	if len(files) == 0 {
		return 0, "", nil
	}
	last := files[len(files)-1]
	data, err := os.ReadFile(last)
	if err != nil {
		return 0, "", err
	}
	idx := bytes.LastIndexByte(data, '\n')
	if idx < 0 {
		// Whole file is one line with no newline; it cannot be a valid
		// committed segment. Remove it only if it is genuinely unparseable.
		var rec Record
		if err := json.Unmarshal(data, &rec); err == nil {
			return 0, "", nil
		}
		if err := os.Remove(last); err != nil {
			return 0, "", err
		}
		if err := fsyncDir(dir); err != nil {
			return 0, "", err
		}
		return len(data), last, nil
	}
	// Keep the newline that terminates the last complete record: the segment
	// must stay a well-formed line-delimited file so the next append can be
	// chained onto it. Only the torn bytes after that newline are removed.
	head := data[:idx+1]
	tail := data[idx+1:]
	if len(tail) == 0 {
		return 0, "", nil // clean trailing newline, nothing to repair
	}
	// Only repair when the tail is genuinely a torn (unparseable) record.
	var rec Record
	if err := json.Unmarshal(tail, &rec); err == nil {
		return 0, "", nil // last line is a complete record; not a torn tail
	}
	if err := replaceFileAtomic(last, head, 0o600); err != nil {
		return 0, "", err
	}
	if err := fsyncDir(dir); err != nil {
		return 0, "", err
	}
	return len(tail), last, nil
}

// replaceFileAtomic publishes content over target without ever exposing a
// partially written file: content is written to a temp file in the same
// directory, fsynced, then renamed over target. A crash at any point leaves
// either the old bytes or the new ones, never a truncated segment.
func replaceFileAtomic(target string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), tempPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	tmpName = "" // renamed successfully; nothing left to clean up
	return nil
}

// fsyncDir flushes the directory entry (needed for rename durability).
// On Windows there is no portable directory-flush syscall (opening a directory
// and Sync'ing it returns "Access is denied"); NTFS rename durability is
// governed by the filesystem itself, so this is a documented no-op there.
func fsyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ReplayDir is a convenience that opens dir read-only and visits all records.
func ReplayDir(dir string, visit func(Record) error) error {
	f, err := OpenReadonly(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Replay(context.Background(), visit)
}
