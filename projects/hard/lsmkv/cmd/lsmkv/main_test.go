package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"lsmkv"
)

func TestReplSession(t *testing.T) {
	dir := t.TempDir()
	session := func(script string) string {
		db, err := lsmkv.Open(dir, lsmkv.Options{Sync: lsmkv.SyncNever})
		require.NoError(t, err)
		var out strings.Builder
		repl(db, strings.NewReader(script), &out, false)
		require.NoError(t, db.Close())
		return out.String()
	}

	got := session(`
put apple red fruit
put banana yellow
put cherry dark red
get apple
get nothing
del banana
scan
scan b
scan a c
flush
stats
bogus
put onlykey
quit
get apple
`)
	require.Equal(t, strings.Join([]string{
		"red fruit",
		"(not found)",
		"apple = red fruit",
		"cherry = dark red",
		"(2 keys)",
		"cherry = dark red",
		"(1 keys)",
		"apple = red fruit",
		"(1 keys)",
		"tables=1 table_bytes=" + tableBytes(got) + " memtable_bytes=0",
		`error: unknown command "bogus" (try help)`,
		"error: usage: put <key> <value>",
		"",
	}, "\n"), got, "nothing after quit runs")

	require.Equal(t, "dark red\n(0 keys)\n", session("get cherry\nscan d\n"), "state survives a reopen")
}

func tableBytes(out string) string {
	_, rest, _ := strings.Cut(out, "table_bytes=")
	n, _, _ := strings.Cut(rest, " ")
	return n
}
