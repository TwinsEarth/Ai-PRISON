package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

// FileStore is the real on-disk append-only JSONL ledger. Records are grouped
// into immutable segment files that are written through a temp file and
// atomically renamed. The full ledger is held in memory for replay while the
// on-disk segments remain the durable source of truth.
type FileStore struct {
	dir      string
	opts     Options
	mode     os.FileMode
	clock    func() time.Time
	readOnly bool

	mu        sync.Mutex
	records   []Record
	persisted int // number of records durably present in published segments
	cleaned   int // leftover temporary files removed on open
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
	records, err := loadSegments(dir, true)
	if err != nil {
		return nil, err
	}
	return &FileStore{
		dir:       dir,
		opts:      opts,
		mode:      mode,
		clock:     opts.Clock,
		records:   records,
		persisted: len(records),
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
func (f *FileStore) Append(_ context.Context, kind string, payload any) (Record, error) {
	if f.readOnly {
		return Record{}, fmt.Errorf("store: %s opened read-only", f.dir)
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return Record{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	seq := uint64(len(f.records))
	prev := ""
	if seq > 0 {
		prev = f.records[seq-1].Hash
	}
	rec := storeRecord(seq, prev, kind, f.clock(), raw)
	f.records = append(f.records, rec)

	switch {
	case f.opts.SyncEveryAppend:
		pending := f.records[f.persisted:]
		if err := f.publishLocked(pending); err != nil {
			return Record{}, err
		}
		f.persisted = len(f.records)
	case len(f.records)-f.persisted >= f.opts.SegmentSize:
		pending := f.records[f.persisted:]
		if err := f.publishLocked(pending); err != nil {
			return Record{}, err
		}
		f.persisted = len(f.records)
	}
	return rec, nil
}

// Sync atomically publishes the buffered active segment.
func (f *FileStore) Sync(_ context.Context) error {
	if f.readOnly {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pending := f.records[f.persisted:]
	if len(pending) == 0 {
		return nil
	}
	if err := f.publishLocked(pending); err != nil {
		return err
	}
	f.persisted = len(f.records)
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

// Replay visits all records in seq order.
func (f *FileStore) Replay(_ context.Context, visit func(Record) error) error {
	f.mu.Lock()
	snapshot := make([]Record, len(f.records))
	copy(snapshot, f.records)
	f.mu.Unlock()
	for _, rec := range snapshot {
		if err := visit(rec); err != nil {
			return err
		}
	}
	return nil
}

// Len returns the number of logical records.
func (f *FileStore) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records)
}

// Persisted reports how many records are durably on disk.
func (f *FileStore) Persisted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.persisted
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

func segmentForSeq(seq uint64, files []string) string {
	for _, p := range files {
		start, ok := segmentStart(p)
		if !ok {
			continue
		}
		if start <= seq {
			return p
		}
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
// repair permitted: it cannot resurrect a complete, tampered record. It
// returns the number of bytes removed and the repaired file.
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
	head := data[:idx]
	tail := data[idx+1:]
	if len(tail) == 0 {
		return 0, "", nil // clean trailing newline, nothing to repair
	}
	// Only repair when the tail is genuinely a torn (unparseable) record.
	var rec Record
	if err := json.Unmarshal(tail, &rec); err == nil {
		return 0, "", nil // last line is a complete record; not a torn tail
	}
	f, err := os.OpenFile(last, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", err
	}
	if _, err := f.Write(head); err != nil {
		_ = f.Close()
		return 0, "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return 0, "", err
	}
	if err := f.Close(); err != nil {
		return 0, "", err
	}
	if err := fsyncDir(dir); err != nil {
		return 0, "", err
	}
	return len(tail), last, nil
}

// fsyncDir flushes the directory entry (needed for rename durability).
func fsyncDir(dir string) error {
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
