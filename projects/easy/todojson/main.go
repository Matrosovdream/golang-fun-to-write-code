// todojson is a minimal todo list in one JSON file.
//
//	todojson add buy milk
//	todojson list
//	todojson done 1
//	todojson rm 1
//
// Subcommands are dispatched by hand on os.Args — do this once, and you'll
// know exactly what cobra (see taskcli) automates and what it costs.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

func main() {
	path := os.Getenv("TODO_FILE")
	if path == "" {
		path = "todos.json"
	}

	store, err := Load(path)
	if err != nil {
		fatal(err)
	}

	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "add":
		if len(args) == 0 {
			fatal(fmt.Errorf("add needs a title"))
		}
		t := store.Add(strings.Join(args, " "))
		saveOr(store)
		fmt.Printf("added #%d: %s\n", t.ID, t.Title)

	case "list", "ls":
		if len(store.Todos) == 0 {
			fmt.Println("nothing to do 🎉")
			return
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, t := range store.Todos {
			mark := " "
			if t.Done() {
				mark = "✓"
			}
			fmt.Fprintf(tw, "%d\t[%s]\t%s\n", t.ID, mark, t.Title)
		}
		tw.Flush()

	case "done":
		t, err := store.MarkDone(parseID(args))
		if err != nil {
			fatal(err)
		}
		saveOr(store)
		fmt.Printf("done #%d: %s ✓\n", t.ID, t.Title)

	case "rm":
		id := parseID(args)
		if err := store.Delete(id); err != nil {
			fatal(err)
		}
		saveOr(store)
		fmt.Printf("deleted #%d\n", id)

	default:
		usage()
	}
}

func parseID(args []string) int {
	if len(args) != 1 {
		fatal(fmt.Errorf("expected exactly one id"))
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		fatal(fmt.Errorf("invalid id %q", args[0]))
	}
	return id
}

func saveOr(s *Store) {
	if err := s.Save(); err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: todojson <add titlewords... | list | done id | rm id>")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "todojson:", err)
	os.Exit(1)
}
