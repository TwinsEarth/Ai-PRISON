package store

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is the in-process append-only ledger. It is the zero-dependency
// backend used by the purely in-memory demo and by the conformance tests. It
// computes the same per-record hashes as FileStore, so a chain built in
// memory can be verified identically.
type MemoryStore struct {
	mu      sync.RWMutex
	records []Record
	clock   func() time.Time
}

// NewMemoryStore returns an empty in-memory ledger.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{clock: time.Now}
}

// NewMemoryStoreWithClock returns an in-memory ledger driven by clock, useful
// for deterministic tests.
func NewMemoryStoreWithClock(clock func() time.Time) *MemoryStore {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryStore{clock: clock}
}

// Append adds the next record and returns it.
func (m *MemoryStore) Append(_ context.Context, kind string, payload any) (Record, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	seq := uint64(len(m.records))
	prev := ""
	if seq > 0 {
		prev = m.records[seq-1].Hash
	}
	rec := storeRecord(seq, prev, kind, m.clock(), raw)
	m.records = append(m.records, rec)
	return rec, nil
}

// Replay visits every record in seq order.
func (m *MemoryStore) Replay(_ context.Context, visit func(Record) error) error {
	m.mu.RLock()
	snapshot := make([]Record, len(m.records))
	copy(snapshot, m.records)
	m.mu.RUnlock()
	for _, rec := range snapshot {
		if err := visit(rec); err != nil {
			return err
		}
	}
	return nil
}

// Sync is a no-op for the memory ledger.
func (m *MemoryStore) Sync(_ context.Context) error { return nil }

// Len returns the number of records.
func (m *MemoryStore) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}

// Path is empty for the memory ledger.
func (m *MemoryStore) Path() string { return "" }

// Close releases nothing for the memory ledger.
func (m *MemoryStore) Close() error { return nil }

// Verify re-derives the hash chain for a detached record slice and reports the
// first tampered or out-of-order record. It is shared by the file loader.
func Verify(records []Record) error {
	for i, rec := range records {
		wantPrev := ""
		if i > 0 {
			wantPrev = records[i-1].Hash
		}
		if rec.Seq != uint64(i) {
			return &IntegrityError{Kind: KindBadSeq, Seq: rec.Seq, Line: i + 1,
				Detail: "non-monotonic sequence"}
		}
		if rec.PrevHash != wantPrev {
			return &IntegrityError{Kind: KindBadLink, Seq: rec.Seq, Line: i + 1,
				Detail: "previous-hash link does not match"}
		}
		if got := hashRecord(rec.Seq, rec.Kind, rec.At, rec.PrevHash, rec.Payload); got != rec.Hash {
			return &IntegrityError{Kind: KindBadHash, Seq: rec.Seq, Line: i + 1,
				Detail: "content digest mismatch"}
		}
	}
	return nil
}
