package raft

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// FileStorage is a write-ahead log of JSON records, one fsync per call. The
// file is only ever appended to; Load replays it from the start.
type FileStorage struct {
	f   *os.File
	enc *json.Encoder
}

type record struct {
	HardState *HardState `json:"hardState,omitempty"`
	Append    []Entry    `json:"append,omitempty"`
	Truncate  uint64     `json:"truncate,omitempty"`
}

func OpenFileStorage(path string) (*FileStorage, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open wal: %w", err)
	}
	return &FileStorage{f: f, enc: json.NewEncoder(f)}, nil
}

func (s *FileStorage) Close() error { return s.f.Close() }

func (s *FileStorage) write(r record) error {
	if err := s.enc.Encode(r); err != nil {
		return fmt.Errorf("write wal: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("sync wal: %w", err)
	}
	return nil
}

func (s *FileStorage) SaveHardState(hs HardState) error { return s.write(record{HardState: &hs}) }
func (s *FileStorage) Append(es []Entry) error          { return s.write(record{Append: es}) }
func (s *FileStorage) TruncateFrom(index uint64) error  { return s.write(record{Truncate: index}) }

func (s *FileStorage) Load() (HardState, []Entry, error) {
	var (
		hs  HardState
		log []Entry
	)
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return hs, nil, fmt.Errorf("seek wal: %w", err)
	}
	dec := json.NewDecoder(s.f)
	for {
		var r record
		err := dec.Decode(&r)
		if errors.Is(err, io.EOF) {
			return hs, log, nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// A crash mid-write leaves a torn last record. It was never
			// acknowledged to anyone, so cut it off and carry on.
			if err := s.f.Truncate(dec.InputOffset()); err != nil {
				return hs, nil, fmt.Errorf("truncate torn wal tail: %w", err)
			}
			return hs, log, nil
		}
		if err != nil {
			return hs, nil, fmt.Errorf("replay wal: %w", err)
		}
		switch {
		case r.HardState != nil:
			hs = *r.HardState
		case r.Truncate > 0:
			log = log[:min(r.Truncate-1, uint64(len(log)))]
		default:
			log = append(log, r.Append...)
		}
	}
}
