package dag

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// graphOf builds tasks from "name: dep dep" shorthand.
func graphOf(t *testing.T, spec ...string) (*Graph, error) {
	t.Helper()
	tasks := map[string]Task{}
	for _, s := range spec {
		name, deps, _ := strings.Cut(s, ":")
		tasks[name] = Task{Cmd: "true", Deps: strings.Fields(deps)}
	}
	return New(tasks)
}

func TestLevels(t *testing.T) {
	g, err := graphOf(t, "deploy: test lint", "test: build", "lint:", "build: gen", "gen:", "docs:")
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"docs", "gen", "lint"},
		{"build"},
		{"test"},
		{"deploy"},
	}, g.Levels())
}

func TestCycleErrorNamesThePath(t *testing.T) {
	tests := []struct {
		name string
		spec []string
		want string
	}{
		{"self loop", []string{"a: a"}, "cycle: a -> a"},
		{"two", []string{"a: b", "b: a"}, "cycle: a -> b -> a"},
		{"three", []string{"a: b", "b: c", "c: a"}, "cycle: a -> b -> c -> a"},
		{"tail leading in", []string{"a: b", "b: c", "c: d", "d: b"}, "cycle: b -> c -> d -> b"},
		{"innocent bystander", []string{"ok:", "x: ok y", "y: x"}, "cycle: x -> y -> x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := graphOf(t, tt.spec...)
			var ce *CycleError
			require.ErrorAs(t, err, &ce)
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestUnknownDependencyIsItsOwnError(t *testing.T) {
	_, err := graphOf(t, "build: gen", "test: build nope", "other: nada")
	var ue *UnknownDepError
	require.ErrorAs(t, err, &ue)
	require.NotErrorAs(t, err, new(*CycleError))
	require.Equal(t, UnknownDepError{"build", "gen"}, *ue)
	// All of them are reported, not just the first.
	require.ErrorContains(t, err, `task "build" depends on unknown task "gen"`)
	require.ErrorContains(t, err, `task "other" depends on unknown task "nada"`)
	require.ErrorContains(t, err, `task "test" depends on unknown task "nope"`)
}

func TestSelectIsTheTransitiveClosure(t *testing.T) {
	g, err := graphOf(t, "deploy: test", "test: build", "build: gen", "gen:", "lint:", "docs: gen")
	require.NoError(t, err)

	sub, err := g.Select("test")
	require.NoError(t, err)
	require.Equal(t, []string{"build", "gen", "test"}, sub.Names())

	sub, err = g.Select("lint", "docs")
	require.NoError(t, err)
	require.Equal(t, []string{"docs", "gen", "lint"}, sub.Names())

	all, err := g.Select()
	require.NoError(t, err)
	require.Len(t, all.Names(), 6)

	_, err = g.Select("test", "ghost")
	require.ErrorIs(t, err, ErrUnknownTask)
	require.ErrorContains(t, err, `"ghost"`)
}

func TestDownstream(t *testing.T) {
	g, err := graphOf(t, "a:", "b: a", "c: b", "d: a c", "e:", "f: e")
	require.NoError(t, err)
	require.Equal(t, []string{"b", "c", "d"}, g.Downstream("a"))
	require.Equal(t, []string{"d"}, g.Downstream("c"))
	require.Empty(t, g.Downstream("d"))
}

func TestDot(t *testing.T) {
	g, err := graphOf(t, "test: build lint", "build:", "lint:")
	require.NoError(t, err)
	require.Equal(t, `digraph dagrun {
	rankdir=LR;
	"build";
	"lint";
	"test";
	"build" -> "test";
	"lint" -> "test";
}
`, g.Dot())
}

func TestParse(t *testing.T) {
	g, err := Parse(strings.NewReader(`
tasks:
  build:
    cmd: go build ./...
    env: {CGO_ENABLED: "0"}
    dir: sub
  test:
    cmd: go test ./...
    deps: [build, build]
`))
	require.NoError(t, err)
	require.Equal(t, Task{Name: "build", Cmd: "go build ./...", Env: map[string]string{"CGO_ENABLED": "0"}, Dir: "sub"}, g.Task("build"))
	require.Equal(t, []string{"build"}, g.Task("test").Deps, "duplicate deps collapse")
}

func TestParseRejects(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"typo in field", "tasks:\n  a:\n    cmd: x\n    dep: [b]\n", "field dep not found"},
		{"missing cmd", "tasks:\n  a:\n    deps: []\n", `task "a": cmd is required`},
		{"bad name", "tasks:\n  'a b':\n    cmd: x\n", `invalid task name "a b"`},
		{"empty file", "", "no tasks defined"},
		{"not yaml", "tasks: [", "parse task file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.yaml))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

// randomGraph wires n tasks with random edges. Edges only point to lower
// indexes unless backEdges is set, so without it the graph is a DAG.
func randomGraph(rng *rand.Rand, n int, backEdges bool) map[string]Task {
	tasks := make(map[string]Task, n)
	for i := range n {
		var deps []string
		for j := range n {
			if j < i && rng.IntN(4) == 0 || backEdges && j >= i && rng.IntN(12) == 0 {
				deps = append(deps, fmt.Sprintf("t%02d", j))
			}
		}
		tasks[fmt.Sprintf("t%02d", i)] = Task{Cmd: "true", Deps: deps}
	}
	return tasks
}

func TestRandomGraphs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		tasks := randomGraph(rng, 2+rng.IntN(14), rng.IntN(2) == 0)
		g, err := New(tasks)

		var ce *CycleError
		if errors.As(err, &ce) {
			// Whatever the reported path is, it must be a real cycle.
			require.Greater(t, len(ce.Path), 1)
			require.Equal(t, ce.Path[0], ce.Path[len(ce.Path)-1])
			for i := 1; i < len(ce.Path); i++ {
				require.Contains(t, tasks[ce.Path[i-1]].Deps, ce.Path[i], "%v", ce.Path)
			}
			continue
		}
		require.NoError(t, err)

		// No cycle: every task is in exactly one level, after all its deps.
		levelOf := map[string]int{}
		for i, level := range g.Levels() {
			for _, n := range level {
				levelOf[n] = i
			}
		}
		require.Len(t, levelOf, len(tasks))
		for n, task := range tasks {
			for _, d := range task.Deps {
				require.Less(t, levelOf[d], levelOf[n])
			}
			// And Downstream agrees with the edges: n is downstream of each dep.
			for _, d := range task.Deps {
				require.True(t, slices.Contains(g.Downstream(d), n))
			}
		}
	}
}
