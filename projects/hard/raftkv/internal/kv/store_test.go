package kv_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"raftkv/internal/kv"
)

func apply(t *testing.T, s *kv.Store, c kv.Command) kv.Result {
	t.Helper()
	b, err := json.Marshal(c)
	require.NoError(t, err)
	res, ok := s.Apply(1, b).(kv.Result)
	require.True(t, ok)
	return res
}

func TestStoreOps(t *testing.T) {
	s := kv.NewStore()
	require.Equal(t, kv.Result{}, apply(t, s, kv.Command{Op: kv.Get, Key: "k"}))
	require.Equal(t, kv.Result{}, apply(t, s, kv.Command{Op: kv.Put, Key: "k", Value: "v1"}))
	require.Equal(t, kv.Result{Value: "v1", Found: true}, apply(t, s, kv.Command{Op: kv.Put, Key: "k", Value: "v2"}))
	require.Equal(t, kv.Result{Value: "v2", Found: true}, apply(t, s, kv.Command{Op: kv.Get, Key: "k"}))
	require.Equal(t, kv.Result{Value: "v2", Found: true}, apply(t, s, kv.Command{Op: kv.Delete, Key: "k"}))
	require.Equal(t, kv.Result{}, apply(t, s, kv.Command{Op: kv.Get, Key: "k"}))
}

func TestStoreDeduplicatesRetries(t *testing.T) {
	s := kv.NewStore()
	a1 := kv.Command{Op: kv.Put, Key: "x", Value: "A", ClientID: "A", Seq: 1}
	first := apply(t, s, a1)
	apply(t, s, kv.Command{Op: kv.Put, Key: "x", Value: "B", ClientID: "B", Seq: 1})

	require.Equal(t, first, apply(t, s, a1), "a retry is answered from the session table")
	require.Equal(t, "B", apply(t, s, kv.Command{Op: kv.Get, Key: "x"}).Value, "and is not executed again")

	a2 := kv.Command{Op: kv.Put, Key: "x", Value: "A2", ClientID: "A", Seq: 2}
	require.Equal(t, kv.Result{Value: "B", Found: true}, apply(t, s, a2), "the next seq runs normally")
	require.Equal(t, "A2", apply(t, s, kv.Command{Op: kv.Get, Key: "x"}).Value)
}

func TestStoreWithoutClientIDNeverDeduplicates(t *testing.T) {
	s := kv.NewStore()
	put := kv.Command{Op: kv.Put, Key: "x", Value: "1"}
	apply(t, s, put)
	apply(t, s, kv.Command{Op: kv.Put, Key: "x", Value: "2"})
	apply(t, s, put)
	require.Equal(t, "1", apply(t, s, kv.Command{Op: kv.Get, Key: "x"}).Value)
}

func TestStoreRejectsGarbage(t *testing.T) {
	err, ok := kv.NewStore().Apply(1, []byte("not json")).(error)
	require.True(t, ok)
	require.Error(t, err)
}

type fakeProposer struct {
	got []byte
	out any
	err error
}

func (f *fakeProposer) Propose(_ context.Context, cmd []byte) (any, error) {
	f.got = cmd
	return f.out, f.err
}

func TestExec(t *testing.T) {
	p := &fakeProposer{out: kv.Result{Value: "v", Found: true}}
	res, err := kv.Exec(context.Background(), p, kv.Command{Op: kv.Get, Key: "k", ClientID: "c", Seq: 7})
	require.NoError(t, err)
	require.Equal(t, kv.Result{Value: "v", Found: true}, res)
	require.JSONEq(t, `{"op":"get","key":"k","client":"c","seq":7}`, string(p.got))

	boom := errors.New("boom")
	_, err = kv.Exec(context.Background(), &fakeProposer{err: boom}, kv.Command{})
	require.ErrorIs(t, err, boom)
	_, err = kv.Exec(context.Background(), &fakeProposer{out: boom}, kv.Command{})
	require.ErrorIs(t, err, boom, "an error returned by Apply is surfaced")
	_, err = kv.Exec(context.Background(), &fakeProposer{out: 42}, kv.Command{})
	require.Error(t, err)
}
