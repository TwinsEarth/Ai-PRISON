package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// testPayload is a representative structured ledger payload.
type testPayload struct {
	AgentID string            `json:"agentId"`
	Level   string            `json:"level"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// backendFactory builds a fresh Store and registers cleanup. The SAME
// conformance suite runs against both the in-memory and the real file backend.
type backendFactory struct {
	name    string
	options Options
	reopen  func(t *testing.T, dir string) (Store, error)
}

func factories() []backendFactory {
	mem := backendFactory{
		name: "memory",
		reopen: func(t *testing.T, _ string) (Store, error) {
			return NewMemoryStore(), nil
		},
	}
	file := backendFactory{
		name: "file",
		options: Options{
			SegmentSize: 4,
		},
	}
	return []backendFactory{mem, file}
}

func newStore(t *testing.T, bf backendFactory) (Store, string) {
	t.Helper()
	if bf.name == "memory" {
		return NewMemoryStore(), ""
	}
	dir := t.TempDir()
	s, err := NewFileStore(dir, bf.options)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s, dir
}

func reopenFile(t *testing.T, dir string) Store {
	t.Helper()
	s, err := NewFileStore(dir, Options{SegmentSize: 4})
	if err != nil {
		t.Fatalf("reopen FileStore: %v", err)
	}
	return s
}

func TestAppendReplayOrderAndHashes(t *testing.T) {
	for _, bf := range factories() {
		t.Run(bf.name, func(t *testing.T) {
			s, dir := newStore(t, bf)
			defer s.Close()
			ctx := context.Background()

			const n = 25
			for i := 0; i < n; i++ {
				p := testPayload{
					AgentID: "agent-" + itoa(i),
					Level:   "L3",
					Meta:    map[string]string{"k": "v", "i": itoa(i)},
				}
				if _, err := s.Append(ctx, KindAuditEvent, p); err != nil {
					t.Fatalf("Append %d: %v", i, err)
				}
			}
			if err := s.Sync(ctx); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			if s.Len() != n {
				t.Fatalf("Len=%d want %d", s.Len(), n)
			}

			var got []Record
			if err := s.Replay(ctx, func(r Record) error { got = append(got, r); return nil }); err != nil {
				t.Fatalf("Replay: %v", err)
			}
			if len(got) != n {
				t.Fatalf("replayed %d want %d", len(got), n)
			}
			if err := Verify(got); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			for i, r := range got {
				if r.Seq != uint64(i) {
					t.Fatalf("seq[%d]=%d", i, r.Seq)
				}
				if r.Hash == "" {
					t.Fatalf("record %d missing hash", i)
				}
				if i > 0 && r.PrevHash != got[i-1].Hash {
					t.Fatalf("record %d not linked", i)
				}
				var p testPayload
				if err := json.Unmarshal(r.Payload, &p); err != nil {
					t.Fatalf("payload %d: %v", i, err)
				}
				if p.AgentID != "agent-"+itoa(i) {
					t.Fatalf("payload roundtrip %d: %+v", i, p)
				}
			}

			if bf.name == "file" {
				// Cross-process-style durability: close and reopen the same dir.
				if err := s.Close(); err != nil {
					t.Fatalf("close: %v", err)
				}
				s2 := reopenFile(t, dir)
				defer s2.Close()
				var got2 []Record
				if err := s2.Replay(ctx, func(r Record) error { got2 = append(got2, r); return nil }); err != nil {
					t.Fatalf("reopen replay: %v", err)
				}
				if len(got2) != n {
					t.Fatalf("after reopen records=%d want %d", len(got2), n)
				}
				if err := Verify(got2); err != nil {
					t.Fatalf("after reopen Verify: %v", err)
				}
			}
		})
	}
}

func TestConcurrentAppendContiguous(t *testing.T) {
	for _, bf := range factories() {
		t.Run(bf.name, func(t *testing.T) {
			s, _ := newStore(t, bf)
			defer s.Close()
			ctx := context.Background()

			const writers = 8
			const per = 50
			var wg sync.WaitGroup
			errs := make(chan error, writers*per)
			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < per; i++ {
						if _, err := s.Append(ctx, KindReceipt, map[string]string{"w": itoa(w), "i": itoa(i)}); err != nil {
							errs <- err
						}
					}
				}()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatalf("append: %v", e)
				}
			}
			if err := s.Sync(ctx); err != nil {
				t.Fatalf("sync: %v", err)
			}
			var got []Record
			if err := s.Replay(ctx, func(r Record) error { got = append(got, r); return nil }); err != nil {
				t.Fatalf("replay: %v", err)
			}
			if len(got) != writers*per {
				t.Fatalf("records=%d want %d", len(got), writers*per)
			}
			seqs := make([]int, len(got))
			for i, r := range got {
				seqs[i] = int(r.Seq)
			}
			sort.Ints(seqs)
			for i, s := range seqs {
				if s != i {
					t.Fatalf("seq gap at %d: got %d", i, s)
				}
			}
			if err := Verify(got); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}

func TestReadonlyRejectsAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, Options{SegmentSize: 2, SyncEveryAppend: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "a"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ro, err := OpenReadonly(dir)
	if err != nil {
		t.Fatalf("readonly open: %v", err)
	}
	if ro.Len() != 1 {
		t.Fatalf("len=%d want 1", ro.Len())
	}
	if _, err := ro.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "b"}); err == nil {
		t.Fatal("expected append on read-only store to fail")
	}
	if err := ro.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestReplayDir(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, Options{SegmentSize: 2})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "x"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	count := 0
	if err := ReplayDir(dir, func(Record) error { count++; return nil }); err != nil {
		t.Fatalf("ReplayDir: %v", err)
	}
	if count != 6 {
		t.Fatalf("ReplayDir count=%d want 6", count)
	}
	// No data files may have been written next to the repo; assert the only
	// files are inside the temp dir.
	if p := filepath.Join(dir, "..", "seg-0000000000.log"); fileExists(p) {
		t.Fatal("ledger wrote outside the data directory")
	}
}
