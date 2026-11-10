package raft_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"raftkv/internal/raft"
)

// disks returns, per implementation, a function that opens "the machine's
// disk" afresh: for the file store that really reopens the file.
func disks(t *testing.T) map[string]func() raft.Storage {
	path := filepath.Join(t.TempDir(), "raft.wal")
	mem := &raft.MemStorage{}
	return map[string]func() raft.Storage{
		"mem": func() raft.Storage { return mem },
		"file": func() raft.Storage {
			s, err := raft.OpenFileStorage(path)
			require.NoError(t, err)
			t.Cleanup(func() { s.Close() })
			return s
		},
	}
}

func e(term uint64, data string) raft.Entry { return raft.Entry{Term: term, Data: []byte(data)} }

func TestStorageRoundTrip(t *testing.T) {
	for name, open := range disks(t) {
		t.Run(name, func(t *testing.T) {
			s := open()
			hs, log, err := s.Load()
			require.NoError(t, err)
			require.Equal(t, raft.HardState{}, hs)
			require.Empty(t, log)

			require.NoError(t, s.SaveHardState(raft.HardState{Term: 1, VotedFor: 3}))
			require.NoError(t, s.SaveHardState(raft.HardState{Term: 4, VotedFor: 2}))
			require.NoError(t, s.Append([]raft.Entry{e(1, "a"), e(1, "b"), e(2, "c")}))
			require.NoError(t, s.TruncateFrom(3))
			require.NoError(t, s.Append([]raft.Entry{e(4, "d")}))
			require.NoError(t, s.TruncateFrom(10)) // past the end: nothing to drop

			hs, log, err = open().Load() // as after a restart
			require.NoError(t, err)
			require.Equal(t, raft.HardState{Term: 4, VotedFor: 2}, hs)
			require.Equal(t, []raft.Entry{e(1, "a"), e(1, "b"), e(4, "d")}, log)

			require.NoError(t, s.TruncateFrom(1))
			_, log, _ = open().Load()
			require.Empty(t, log)
		})
	}
}

func TestFileStorageDropsTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raft.wal")
	s, err := raft.OpenFileStorage(path)
	require.NoError(t, err)
	require.NoError(t, s.Append([]raft.Entry{e(1, "a"), e(1, "b")}))
	require.NoError(t, s.Close())

	// A crash in the middle of the next write leaves half a record behind.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(`{"append":[{"term":1,"da`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	s, err = raft.OpenFileStorage(path)
	require.NoError(t, err)
	_, log, err := s.Load()
	require.NoError(t, err)
	require.Equal(t, []raft.Entry{e(1, "a"), e(1, "b")}, log, "the unacknowledged torn record is ignored")

	require.NoError(t, s.Append([]raft.Entry{e(2, "c")}))
	require.NoError(t, s.Close())
	s, err = raft.OpenFileStorage(path)
	require.NoError(t, err)
	defer s.Close()
	_, log, err = s.Load()
	require.NoError(t, err)
	require.Equal(t, []raft.Entry{e(1, "a"), e(1, "b"), e(2, "c")}, log, "and later writes land cleanly after it")
}
