package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"taskcli/internal/storage"
	"taskcli/internal/task"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List tasks (open only by default)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")

		var opts []storage.ListOption
		if !all {
			opts = append(opts, storage.WithStatus(task.StatusOpen))
		}
		if limit > 0 {
			opts = append(opts, storage.WithLimit(limit))
		}

		tasks, err := repo.List(cmd.Context(), opts...)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			fmt.Println("no tasks — add one with: taskcli add \"do something\"")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tPRI\tSTATUS\tAGE\tTITLE")
		for _, t := range tasks {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
				t.ID, prioLabel(t.Priority), t.Status, age(t.CreatedAt), t.Title)
		}
		return w.Flush()
	},
}

func init() {
	listCmd.Flags().BoolP("all", "a", false, "include completed tasks")
	listCmd.Flags().IntP("limit", "n", 0, "show at most N tasks")
	rootCmd.AddCommand(listCmd)
}

func prioLabel(p int) string {
	switch p {
	case 1:
		return "high"
	case 3:
		return "low"
	default:
		return "norm"
	}
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
