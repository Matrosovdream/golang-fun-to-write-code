package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"crdtpad/internal/rga"
)

// oplog is the durable copy of a document: every applied op, one JSON object
// per line, in application order (which is a valid causal order). Replaying
// it through rga.Doc.Apply rebuilds the document exactly.
//
// Appends are write(2) calls without fsync: they survive the process dying,
// not the machine losing power. A torn final line after a power cut makes
// the next start fail loudly instead of guessing.
type oplog struct {
	f   *os.File
	enc *json.Encoder
}

// openOplog opens (creating if needed) the file at path and feeds every
// stored op to replay before returning.
func openOplog(path string, replay func(rga.Op) error) (*oplog, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open oplog: %w", err)
	}
	dec := json.NewDecoder(f)
	for n := 1; ; n++ {
		var op rga.Op
		if err := dec.Decode(&op); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			f.Close()
			return nil, fmt.Errorf("oplog %s: op %d: %w", path, n, err)
		}
		if err := replay(op); err != nil {
			f.Close()
			return nil, fmt.Errorf("oplog %s: op %d: %w", path, n, err)
		}
	}
	return &oplog{f: f, enc: json.NewEncoder(f)}, nil
}

func (l *oplog) Append(op rga.Op) error {
	if err := l.enc.Encode(op); err != nil {
		return fmt.Errorf("append to oplog: %w", err)
	}
	return nil
}

func (l *oplog) Close() error { return l.f.Close() }
