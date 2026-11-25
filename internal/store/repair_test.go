package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepairTornTailRestoresExactBytes locks the byte-level contract of the
// only permitted integrity repair: it removes exactly the torn bytes, keeps
// the newline that terminates the last committed record, leaves no temp file
// behind, and leaves the ledger immediately appendable.
func TestRepairTornTailRestoresExactBytes(t *testing.T) {
	dir := t.TempDir()
	writeAndClose(t, dir, Options{}, 3, nil)

	seg := filepath.Join(dir, "seg-0000000000.log")
	clean, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	torn := []byte(`{"seq":`)
	withTorn := append(append([]byte(nil), clean...), torn...)
	if err := os.WriteFile(seg, withTorn, 0o600); err != nil {
		t.Fatalf("write torn segment: %v", err)
	}

	removed, file, err := RepairTornTail(dir)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if file != seg {
		t.Fatalf("repaired file = %q, want %q", file, seg)
	}
	if removed != len(torn) {
		t.Fatalf("removed = %d bytes, want exactly %d", removed, len(torn))
	}

	got, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read repaired segment: %v", err)
	}
	if !bytes.Equal(got, clean) {
		t.Fatalf("repaired segment differs from the pre-tear state:\n got %d bytes\nwant %d bytes", len(got), len(clean))
	}
	if last := got[len(got)-1]; last != '\n' {
		t.Fatalf("repaired segment must end with '\\n', got %q", last)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), tempSuffixPattern) {
			t.Errorf("repair left a temp file behind: %s", entry.Name())
		}
	}

	// The repaired ledger must reopen, stay appendable and verify.
	s, err := NewFileStore(dir, Options{})
	if err != nil {
		t.Fatalf("reopen after repair: %v", err)
	}
	if _, err := s.Append(context.Background(), KindAuditEvent, testPayload{AgentID: "agent-after", Level: "L3"}); err != nil {
		t.Fatalf("append after repair: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close after repair: %v", err)
	}
	ro, err := OpenReadonly(dir)
	if err != nil {
		t.Fatalf("verify after repair: %v", err)
	}
	n := ro.Len()
	_ = ro.Close()
	if n != 4 {
		t.Fatalf("records after repair = %d, want 4", n)
	}
}

// TestRepairTornTailLongerTail removes a torn tail that is longer and carries
// embedded escape sequences (a partially written JSON record can contain a
// literal backslash-n inside a string value without introducing a real line
// break), and still restores the pre-tear bytes exactly.
func TestRepairTornTailLongerTail(t *testing.T) {
	dir := t.TempDir()
	writeAndClose(t, dir, Options{}, 2, nil)

	seg := filepath.Join(dir, "seg-0000000000.log")
	clean, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	torn := []byte(`{"seq":1,"hash":"partial\nx","note":"unterminated`)
	if err := os.WriteFile(seg, append(append([]byte(nil), clean...), torn...), 0o600); err != nil {
		t.Fatalf("write torn segment: %v", err)
	}

	removed, _, err := RepairTornTail(dir)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if removed != len(torn) {
		t.Fatalf("removed = %d bytes, want %d", removed, len(torn))
	}
	got, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read repaired segment: %v", err)
	}
	if !bytes.Equal(got, clean) {
		t.Fatalf("repaired segment differs from the pre-tear state")
	}
}

// tempSuffixPattern is the suffix half of tempPattern, used for leftover checks.
const tempSuffixPattern = ".tmp"

// TestIntegrityErrorNamesTheRightSegment locks the fault-location contract for
// multi-segment ledgers: an integrity failure at seq N must name the segment
// that actually contains N, not the first segment whose start is <= N.
func TestIntegrityErrorNamesTheRightSegment(t *testing.T) {
	dir := t.TempDir()
	writeAndClose(t, dir, Options{SegmentSize: 2}, 5, nil)

	// seq 3 lives in seg-0000000002.log (segments start at 0, 2, 4).
	seg := filepath.Join(dir, "seg-0000000002.log")
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	tampered := bytes.Replace(data, []byte(`"agent-3"`), []byte(`"agent-X"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatalf("tamper marker not found in %s", seg)
	}
	if err := os.WriteFile(seg, tampered, 0o600); err != nil {
		t.Fatalf("write tampered segment: %v", err)
	}

	_, err = OpenReadonly(dir)
	ie := AsIntegrityError(err)
	if ie == nil {
		t.Fatalf("expected an integrity error, got %v", err)
	}
	if ie.Seq != 3 {
		t.Fatalf("reported seq = %d, want 3", ie.Seq)
	}
	if ie.File != seg {
		t.Fatalf("reported file = %s, want the segment that holds seq 3 (%s)", ie.File, seg)
	}
}
