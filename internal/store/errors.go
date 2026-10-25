package store

import (
	"errors"
	"fmt"
)

// IntegrityError marks every error that means the on-disk ledger cannot be
// trusted as a faithful, complete record (tampering or a torn tail).
type IntegrityError struct {
	// Kind is "hash" (content digest mismatch), "link" (broken previous-hash
	// link), "seq" (non-monotonic sequence number) or "torn" (an incomplete
	// final line that cannot be parsed).
	Kind string
	// File is the segment path containing the offending record, or the data
	// directory when the fault is not tied to a segment.
	File string
	// Line is the 1-based line number inside the segment when known.
	Line int
	// Seq is the expected monotonic sequence number of the offending record.
	Seq uint64
	// Offset is the byte offset of the offending line/tail when known.
	Offset int64
	// Detail is a human-readable explanation.
	Detail string
}

func (e *IntegrityError) Error() string {
	switch e.Kind {
	case KindTornTail:
		return fmt.Sprintf("store: torn tail in %s at line %d (offset %d): %s",
			e.File, e.Line, e.Offset, e.Detail)
	default:
		return fmt.Sprintf("store: %s mismatch at seq %d (%s line %d): %s",
			e.Kind, e.Seq, e.File, e.Line, e.Detail)
	}
}

// Integrity error Kinds.
const (
	KindBadHash  = "hash"
	KindBadLink  = "link"
	KindBadSeq   = "seq"
	KindCorrupt  = "corrupt"
	KindTornTail = "torn"
)

// AsIntegrityError returns the *IntegrityError inside err, or nil.
func AsIntegrityError(err error) *IntegrityError {
	var ie *IntegrityError
	if errors.As(err, &ie) {
		return ie
	}
	return nil
}

// IsTornTail reports whether err is specifically a torn (incomplete) tail,
// which is the only integrity fault that may be repaired by truncation.
func IsTornTail(err error) bool {
	if ie := AsIntegrityError(err); ie != nil {
		return ie.Kind == KindTornTail
	}
	return false
}

// IsTamper reports whether err is a hard tampering fault (a complete record
// whose hash, link or sequence is wrong). Such faults are never auto-repaired.
func IsTamper(err error) bool {
	if ie := AsIntegrityError(err); ie != nil {
		return ie.Kind != KindTornTail
	}
	return false
}
