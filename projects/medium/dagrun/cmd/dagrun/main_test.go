package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"dagrun/internal/dag"
)

const taskFile = `
tasks:
  gen:   {cmd: "true"}
  lint:  {cmd: "true", deps: [gen]}
  build: {cmd: "true", deps: [gen]}
  test:  {cmd: "false", deps: [build]}
  ship:  {cmd: "true", deps: [test, lint]}
  docs:  {cmd: "true"}
`

// stub is an Executor that echoes the task name and fails when cmd is "false".
type stub struct{}

func (stub) Run(_ context.Context, t dag.Task, out io.Writer) error {
	fmt.Fprintf(out, "ran %s\n", t.Name)
	if t.Cmd == "false" {
		return errors.New("exit status 1")
	}
	return nil
}

func exec(t *testing.T, content string, args ...string) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dagrun.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	var out bytes.Buffer
	err := run(t.Context(), append([]string{"-f", path}, args...), &out, io.Discard, stub{})
	return out.String(), err
}

func TestDryRunPrintsLevels(t *testing.T) {
	out, err := exec(t, taskFile, "--dry-run")
	require.NoError(t, err)
	require.Equal(t, "level 1: docs gen\nlevel 2: build lint\nlevel 3: test\nlevel 4: ship\n", out)
}

func TestDryRunOnlyShowsWhatTheTargetNeeds(t *testing.T) {
	out, err := exec(t, taskFile, "--dry-run", "build")
	require.NoError(t, err)
	require.Equal(t, "level 1: gen\nlevel 2: build\n", out)
}

func TestDot(t *testing.T) {
	out, err := exec(t, taskFile, "--dot", "lint")
	require.NoError(t, err)
	require.Equal(t, "digraph dagrun {\n\trankdir=LR;\n\t\"gen\";\n\t\"lint\";\n\t\"gen\" -> \"lint\";\n}\n", out)
}

func TestConfigErrors(t *testing.T) {
	_, err := exec(t, "tasks:\n  a: {cmd: x, deps: [b]}\n  b: {cmd: x, deps: [c]}\n  c: {cmd: x, deps: [a]}\n")
	require.ErrorContains(t, err, "cycle: a -> b -> c -> a")

	_, err = exec(t, "tasks:\n  a: {cmd: x, deps: [zzz]}\n")
	require.ErrorContains(t, err, `task "a" depends on unknown task "zzz"`)

	_, err = exec(t, taskFile, "nope")
	require.ErrorIs(t, err, dag.ErrUnknownTask)

	_, err = exec(t, taskFile, "-j", "0")
	require.ErrorContains(t, err, "-j must be at least 1")
}

func TestFailureExitsNonZeroAndSkipsDependents(t *testing.T) {
	out, err := exec(t, taskFile, "-j", "1", "-k")
	require.ErrorContains(t, err, "test: exit status 1")
	require.Contains(t, out, "[test ] FAILED")
	require.Contains(t, out, `[ship ] skipped: dependency "test" failed`)
	require.Contains(t, out, "[docs ] ran docs")
	require.Contains(t, out, "4 ok, 1 failed, 1 skipped in ")
}

func TestSuccessIsNil(t *testing.T) {
	out, err := exec(t, taskFile, "-j", "2", "lint", "docs")
	require.NoError(t, err)
	require.Contains(t, out, "3 ok, 0 failed, 0 skipped in ")
}
