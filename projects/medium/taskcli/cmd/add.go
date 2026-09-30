package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"taskcli/internal/task"
)

var addCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Add a task",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prio, _ := cmd.Flags().GetInt("priority")
		if prio < 1 || prio > 3 {
			return fmt.Errorf("priority must be 1..3, got %d", prio)
		}

		t := &task.Task{
			Title:     strings.Join(args, " "),
			Priority:  prio,
			Status:    task.StatusOpen,
			CreatedAt: time.Now(),
		}
		if err := repo.Add(cmd.Context(), t); err != nil {
			return err
		}
		fmt.Printf("added #%d: %s\n", t.ID, t.Title)
		return nil
	},
}

func init() {
	addCmd.Flags().IntP("priority", "p", 2, "1 = high, 2 = normal, 3 = low")
	rootCmd.AddCommand(addCmd)
}
