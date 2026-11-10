// Command lsmkv is a tiny REPL over an lsmkv database directory.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"lsmkv"
)

const help = `put <key> <value...>   store a value (the rest of the line)
get <key>              print a value
del <key>              delete a key
scan [lo [hi]]         list keys in [lo, hi)
flush                  write the memtable to an SSTable
compact                merge all SSTables into one
stats                  table count and sizes
quit`

func main() {
	dir := flag.String("dir", "lsmkv-data", "database directory")
	syncName := flag.String("sync", "every", "WAL fsync policy: every, periodic or never")
	flag.Parse()

	policies := map[string]lsmkv.SyncPolicy{"every": lsmkv.SyncEvery, "periodic": lsmkv.SyncPeriodic, "never": lsmkv.SyncNever}
	policy, ok := policies[*syncName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown -sync %q\n", *syncName)
		os.Exit(2)
	}
	db, err := lsmkv.Open(*dir, lsmkv.Options{Sync: policy})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	st, _ := os.Stdin.Stat()
	repl(db, os.Stdin, os.Stdout, st.Mode()&os.ModeCharDevice != 0)
	if err := db.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// repl runs commands until quit or EOF. Command errors are printed, not fatal.
func repl(db *lsmkv.DB, in io.Reader, out io.Writer, prompt bool) {
	sc := bufio.NewScanner(in)
	for {
		if prompt {
			fmt.Fprint(out, "> ")
		}
		if !sc.Scan() {
			return
		}
		cmd, rest, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if cmd == "quit" || cmd == "exit" {
			return
		}
		if err := run(db, out, cmd, strings.TrimSpace(rest)); err != nil {
			fmt.Fprintln(out, "error:", err)
		}
	}
}

func run(db *lsmkv.DB, out io.Writer, cmd, args string) error {
	switch cmd {
	case "":
	case "put":
		key, val, ok := strings.Cut(args, " ")
		if key == "" || !ok {
			return fmt.Errorf("usage: put <key> <value>")
		}
		return db.Put(key, []byte(strings.TrimSpace(val)))
	case "get":
		v, err := db.Get(args)
		if errors.Is(err, lsmkv.ErrNotFound) {
			fmt.Fprintln(out, "(not found)")
			return nil
		}
		fmt.Fprintf(out, "%s\n", v)
		return err
	case "del":
		return db.Delete(args)
	case "scan":
		lo, hi, _ := strings.Cut(args, " ")
		n := 0
		seq, scanErr := db.Scan(lo, strings.TrimSpace(hi))
		for k, v := range seq {
			fmt.Fprintf(out, "%s = %s\n", k, v)
			n++
		}
		fmt.Fprintf(out, "(%d keys)\n", n)
		return scanErr()
	case "flush":
		return db.Flush()
	case "compact":
		return db.Compact()
	case "stats":
		s := db.Stats()
		fmt.Fprintf(out, "tables=%d table_bytes=%d memtable_bytes=%d\n", s.Tables, s.TableBytes, s.MemtableBytes)
	case "help":
		fmt.Fprintln(out, help)
	default:
		return fmt.Errorf("unknown command %q (try help)", cmd)
	}
	return nil
}
