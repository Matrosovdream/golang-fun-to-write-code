package rga

import (
	"cmp"
	"fmt"
)

// ID names one inserted character for the lifetime of the document.
// Order is lexicographic: Lamport clock first, replica name as tie-break.
type ID struct {
	Clock   uint64 `json:"clock"`
	Replica string `json:"replica"`
}

func (a ID) Compare(b ID) int {
	return cmp.Or(cmp.Compare(a.Clock, b.Clock), cmp.Compare(a.Replica, b.Replica))
}

type Kind string

const (
	KindInsert Kind = "ins"
	KindDelete Kind = "del"
)

// Op is the unit of replication. Every op is named by (Replica, Seq), where
// Seq counts that replica's ops 1, 2, 3, ... with no gaps: that is what makes
// a version vector ("highest Seq I have from each replica") a complete
// description of what a replica knows.
//
// An insert additionally carries a Lamport Clock; the new character's ID is
// (Clock, Replica). Deletes carry only the Target.
type Op struct {
	Replica string `json:"replica"`
	Seq     uint64 `json:"seq"`
	Kind    Kind   `json:"kind"`
	Clock   uint64 `json:"clock,omitempty"`
	After   ID     `json:"after,omitzero"`
	Char    rune   `json:"char,omitempty"`
	Target  ID     `json:"target,omitzero"`
}

// ID is the character an insert creates.
func (o Op) ID() ID { return ID{o.Clock, o.Replica} }

// ValidReplica reports whether s may name a replica. The alphabet is ASCII
// on purpose: ID order compares replica names, and Go (bytes) and JavaScript
// (UTF-16 units) only agree on that order for ASCII.
func ValidReplica(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		ok := c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !ok {
			return false
		}
	}
	return true
}

func (o Op) validate() error {
	switch {
	case !ValidReplica(o.Replica):
		return fmt.Errorf("rga: invalid replica %q", o.Replica)
	case o.Seq == 0:
		return fmt.Errorf("rga: op from %s has seq 0", o.Replica)
	}
	switch o.Kind {
	case KindInsert:
		// The scan rule in integrate is only correct if an insert's ID is
		// greater than its parent's, which every honest Lamport clock ensures.
		if o.Clock == 0 || o.After.Compare(o.ID()) >= 0 {
			return fmt.Errorf("rga: insert %s/%d has clock %d, not above its parent %v", o.Replica, o.Seq, o.Clock, o.After)
		}
	case KindDelete:
		if o.Target == (ID{}) {
			return fmt.Errorf("rga: delete %s/%d has no target", o.Replica, o.Seq)
		}
	default:
		return fmt.Errorf("rga: unknown op kind %q", o.Kind)
	}
	return nil
}
