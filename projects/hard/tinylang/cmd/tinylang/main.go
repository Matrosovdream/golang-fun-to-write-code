// Command tinylang runs a program file, an embedded example, or a REPL.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"tinylang"
)

func main() {
	engine := flag.String("engine", "vm", "execution engine: eval or vm")
	maxSteps := flag.Int("max-steps", 0, "abort after this many calls+loop iterations (0 = no limit)")
	example := flag.String("example", "", "run an embedded example from testdata/, e.g. -example fib")
	flag.Parse()
	if err := run(os.Stdin, os.Stdout, *engine, *maxSteps, *example, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer, engine string, maxSteps int, example string, files []string) error {
	e, err := tinylang.NewEngine(engine, out, maxSteps)
	if err != nil {
		return err
	}
	var src []byte
	switch {
	case example != "":
		src, err = tinylang.Examples.ReadFile("testdata/" + example + ".tl")
	case len(files) == 1:
		src, err = os.ReadFile(files[0])
	case len(files) == 0:
		return repl(in, out, e)
	default:
		return errors.New("usage: tinylang [-engine eval|vm] [-max-steps n] [file.tl]")
	}
	if err != nil {
		return err
	}
	_, err = tinylang.Run(e, string(src))
	return err
}
