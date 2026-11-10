// Package dag turns a task file into a validated dependency graph and answers
// the questions a runner asks of it: what runs when, what does a target need,
// and what is lost if a task fails.
//
// An arrow in this package always reads "depends on": a -> b means a needs b.
package dag

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

type Task struct {
	Name string            `yaml:"-"`
	Cmd  string            `yaml:"cmd"`
	Deps []string          `yaml:"deps"`
	Env  map[string]string `yaml:"env"`
	Dir  string            `yaml:"dir"`
}

var (
	ErrUnknownTask = errors.New("unknown task")

	// Names end up in terminal prefixes and quoted DOT identifiers, so keep them boring.
	validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// UnknownDepError reports a dependency that names no task.
type UnknownDepError struct{ Task, Dep string }

func (e *UnknownDepError) Error() string {
	return fmt.Sprintf("task %q depends on unknown task %q", e.Task, e.Dep)
}

// CycleError carries the cycle itself: Path starts and ends on the same task.
type CycleError struct{ Path []string }

func (e *CycleError) Error() string { return "cycle: " + strings.Join(e.Path, " -> ") }

type Graph struct {
	tasks      map[string]Task
	names      []string            // sorted: every iteration order in this package is deterministic
	dependents map[string][]string // reverse edges, sorted
}

// New validates tasks and builds the graph. Unknown dependencies are all
// reported together; a cycle is reported with its path.
func New(tasks map[string]Task) (*Graph, error) {
	if len(tasks) == 0 {
		return nil, errors.New("no tasks defined")
	}
	g := &Graph{tasks: make(map[string]Task, len(tasks)), dependents: map[string][]string{}}
	var unknown []error
	for _, name := range slices.Sorted(maps.Keys(tasks)) {
		t := tasks[name]
		if !validName.MatchString(name) {
			return nil, fmt.Errorf("invalid task name %q", name)
		}
		if strings.TrimSpace(t.Cmd) == "" {
			return nil, fmt.Errorf("task %q: cmd is required", name)
		}
		t.Name = name
		t.Deps = slices.Compact(slices.Sorted(slices.Values(t.Deps)))
		g.tasks[name] = t
		g.names = append(g.names, name)
		for _, d := range t.Deps {
			if _, ok := tasks[d]; !ok {
				unknown = append(unknown, &UnknownDepError{name, d})
			}
			g.dependents[d] = append(g.dependents[d], name)
		}
	}
	if len(unknown) > 0 {
		return nil, errors.Join(unknown...)
	}
	if _, stuck := g.kahn(); len(stuck) > 0 {
		return nil, &CycleError{g.cycle(stuck)}
	}
	return g, nil
}

func (g *Graph) Names() []string              { return g.names }
func (g *Graph) Task(name string) Task        { return g.tasks[name] }
func (g *Graph) Dependents(n string) []string { return g.dependents[n] }

// Levels groups tasks so that every task's dependencies sit in earlier levels;
// all tasks of one level could run in parallel.
func (g *Graph) Levels() [][]string {
	levels, _ := g.kahn()
	return levels
}

// kahn is Kahn's algorithm, one level at a time: level 0 has no dependencies,
// and finishing a level lowers the indegree of its dependents. Whatever never
// reaches indegree 0 is stuck on, or behind, a cycle.
func (g *Graph) kahn() (levels [][]string, stuck []string) {
	indeg := make(map[string]int, len(g.names))
	var level []string
	for _, n := range g.names {
		if indeg[n] = len(g.tasks[n].Deps); indeg[n] == 0 {
			level = append(level, n)
		}
	}
	for len(level) > 0 {
		levels = append(levels, level)
		var next []string
		for _, n := range level {
			for _, d := range g.dependents[n] {
				if indeg[d]--; indeg[d] == 0 {
					next = append(next, d)
				}
			}
		}
		slices.Sort(next)
		level = next
	}
	for _, n := range g.names {
		if indeg[n] > 0 {
			stuck = append(stuck, n)
		}
	}
	return levels, stuck
}

// cycle extracts a concrete cycle from the stuck set. Every stuck task has at
// least one stuck dependency (that is why it never became ready), so walking
// "first stuck dependency" must eventually revisit a task, and the walk from
// that first visit onward is the cycle.
func (g *Graph) cycle(stuck []string) []string {
	path := []string{stuck[0]}
	at := map[string]int{stuck[0]: 0}
	for cur := stuck[0]; ; {
		deps := g.tasks[cur].Deps
		next := deps[slices.IndexFunc(deps, func(d string) bool { return slices.Contains(stuck, d) })]
		if i, seen := at[next]; seen {
			return append(path[i:], next)
		}
		at[next] = len(path)
		path = append(path, next)
		cur = next
	}
}

// Select returns the subgraph that targets need: the targets plus everything
// they transitively depend on. No targets means the whole graph.
func (g *Graph) Select(targets ...string) (*Graph, error) {
	if len(targets) == 0 {
		return g, nil
	}
	for _, t := range targets {
		if _, ok := g.tasks[t]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTask, t)
		}
	}
	sub := map[string]Task{}
	for n := range closure(targets, func(n string) []string { return g.tasks[n].Deps }) {
		sub[n] = g.tasks[n]
	}
	return New(sub)
}

// Downstream lists every task that transitively depends on name: exactly the
// tasks that cannot run if name fails.
func (g *Graph) Downstream(name string) []string {
	return slices.Sorted(maps.Keys(closure(g.dependents[name], func(n string) []string { return g.dependents[n] })))
}

// closure is a depth-first reachability walk from start (inclusive).
func closure(start []string, next func(string) []string) map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(start)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !seen[n] {
			seen[n] = true
			stack = append(stack, next(n)...)
		}
	}
	return seen
}

// Dot renders the graph as Graphviz, with edges pointing in execution order
// (dependency -> dependent).
func (g *Graph) Dot() string {
	var b strings.Builder
	b.WriteString("digraph dagrun {\n\trankdir=LR;\n")
	for _, n := range g.names {
		fmt.Fprintf(&b, "\t%q;\n", n)
	}
	for _, n := range g.names {
		for _, d := range g.tasks[n].Deps {
			fmt.Fprintf(&b, "\t%q -> %q;\n", d, n)
		}
	}
	b.WriteString("}\n")
	return b.String()
}
