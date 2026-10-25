package migration

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// A custodian's simulated "share token" is the deterministic string
// "<contentHash>|share-<i>" assigned at seal time. This is NOT real Shamir
// secret sharing; the only thing enforced is the M-of-N threshold bookkeeping.
// Tokens are stored inline in sealedCustodyRow.shares (custodian -> token).

type sealedCustodyRow struct {
	SealedCustody
	content string            // the simulated payload behind the threshold (its hash is what leaves)
	shares  map[string]string // custodian -> expected simulated token
}

// MemCustodyStore is the in-memory CustodyStore. It simulates sealing a
// pack_diff snapshot across N custodians behind an M-of-N threshold:
//
//   - there is NO real threshold cryptography / multi-party encryption — a
//     "share token" is just a deterministic string;
//   - unseal genuinely requires >= `threshold` distinct, valid custodian
//     shares; below it the content stays locked, so a migrated copy cannot
//     inherit the accumulated collaboration state / intermediate results.
//
// Every unseal attempt (success or failure) is written to the judgement chain
// via the injected EventSink, so attempted-but-threshold-failing unseals leave
// an audit trail.
type MemCustodyStore struct {
	mu     sync.Mutex
	items  map[string]sealedCustodyRow
	events EventSink
	now    func() time.Time
}

// NewMemCustodyStore returns an empty custody store. A nil EventSink is
// accepted (attempts are still statefully recorded) but production wiring
// should pass NewChainSink(chain) so access is audited.
func NewMemCustodyStore(events EventSink) *MemCustodyStore {
	if events == nil {
		events = noopEvents{}
	}
	return &MemCustodyStore{items: make(map[string]sealedCustodyRow), events: events, now: time.Now}
}

// Seal splits the snapshot across the named custodians behind an M-of-N
// threshold. Re-sealing the same id returns the existing record unchanged
// (idempotent). The simulated shares are generated deterministically from the
// content hash — again, NOT real secret sharing.
func (s *MemCustodyStore) Seal(id string, agentID domain.AgentID, contentHash string, threshold int, custodians []string) (SealedCustody, error) {
	if id == "" {
		return SealedCustody{}, fmt.Errorf("migration: cannot seal a snapshot with an empty id")
	}
	if contentHash == "" {
		return SealedCustody{}, fmt.Errorf("migration: cannot seal snapshot %q without a content hash", id)
	}
	uniq := uniqStrings(custodians)
	if threshold <= 0 {
		threshold = len(uniq)
	}
	if threshold > len(uniq) {
		return SealedCustody{}, fmt.Errorf("migration: unseal threshold %d exceeds %d custodians", threshold, len(uniq))
	}
	if len(uniq) == 0 {
		return SealedCustody{}, fmt.Errorf("migration: snapshot %q needs at least one simulated custodian", id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.items[id]; ok {
		return existing.SealedCustody, nil
	}

	shares := make(map[string]string, len(uniq))
	for i, c := range uniq {
		shares[c] = fmt.Sprintf("%s|share-%d", contentHash, i+1)
	}
	row := sealedCustodyRow{
		SealedCustody: SealedCustody{
			ID:          id,
			AgentID:     agentID,
			ContentHash: contentHash,
			Threshold:   threshold,
			Total:       len(uniq),
			Custodians:  uniq,
			SealedAt:    s.now().UTC(),
		},
		content: contentHash,
		shares:  shares,
	}
	s.items[id] = row

	s.events.Append(domain.Event{
		Type:    domain.EventSnapshotSealed,
		AgentID: agentID,
		Actor:   "migration-custody",
		Payload: map[string]string{
			"snapshotId":  id,
			"contentHash": contentHash,
			"thresholdM":  fmt.Sprintf("%d", threshold),
			"totalN":      fmt.Sprintf("%d", len(uniq)),
			"simulated":   "true",
		},
	})
	return row.SealedCustody, nil
}

// Unseal accepts shares and returns the content hash iff at least `threshold`
// distinct, valid custodian shares are presented. A share counts only when its
// custodian is a registered holder AND its token matches the simulated token
// assigned at seal time. Below the threshold the content stays locked and the
// attempt is audited.
func (s *MemCustodyStore) Unseal(id string, shares []Share) (string, error) {
	s.mu.Lock()
	row, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		s.auditUnseal(id, "", 0, 0, false, "unknown snapshot")
		return "", fmt.Errorf("migration: unknown sealed snapshot %q", id)
	}

	valid := make(map[string]struct{})
	for _, sh := range shares {
		want, isHolder := row.shares[sh.Custodian]
		if !isHolder || sh.Token == "" || sh.Token != want {
			continue // an invalid / wrong-holder share does not count
		}
		valid[sh.Custodian] = struct{}{}
	}
	got := len(valid)
	if got < row.Threshold {
		s.auditUnseal(id, row.ContentHash, got, row.Threshold, false, "below M-of-N threshold")
		return "", fmt.Errorf("migration: unseal threshold not met: %d of %d required valid shares (SIMULATED custody)", got, row.Threshold)
	}
	s.auditUnseal(id, row.ContentHash, got, row.Threshold, true, "threshold met")
	return row.content, nil
}

// auditUnseal records every unseal attempt (success OR failure) on the chain.
func (s *MemCustodyStore) auditUnseal(id, contentHash string, got, threshold int, ok bool, reason string) {
	payload := map[string]string{
		"snapshotId":       id,
		"attempt":          "unseal",
		"validShares":      fmt.Sprintf("%d", got),
		"thresholdM":       fmt.Sprintf("%d", threshold),
		"simulated":        "true",
		"simulatedCustody": "true",
	}
	if ok {
		payload["result"] = "unsealed"
		payload["contentHash"] = contentHash
	} else {
		payload["result"] = "denied"
		payload["reason"] = reason
	}
	s.events.Append(domain.Event{
		Type:    domain.EventSnapshotSealed, // access to a sealed snapshot (seal OR unseal attempt)
		Actor:   "migration-custody",
		Payload: payload,
	})
}

// Get returns a detached sealed custody record by id.
func (s *MemCustodyStore) Get(id string) (SealedCustody, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.items[id]
	if !ok {
		return SealedCustody{}, false
	}
	return row.SealedCustody, true
}

// ShareToken returns the simulated share token a custodian holds, as if that
// custodian were presenting their share to unseal. It is the read side a real
// custodian would use; the token itself is a deterministic label, NOT a real
// secret-share fragment. ok=false for an unknown snapshot or custodian.
func (s *MemCustodyStore) ShareToken(id, custodian string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.items[id]
	if !ok {
		return "", false
	}
	tok, ok := row.shares[custodian]
	return tok, ok
}

// Len returns the number of sealed snapshots.
func (s *MemCustodyStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// uniqStrings dedupes and sorts custodian names deterministically.
func uniqStrings(in []string) []string {
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		if s != "" {
			set[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
