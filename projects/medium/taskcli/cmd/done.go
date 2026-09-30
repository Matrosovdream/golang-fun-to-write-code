package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var doneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		if err := repo.MarkDone(cmd.Context(), id); err != nil {
			return err
		}
		fmt.Printf("done #%d ✔\n", id)
		return nil
	},
}

var rmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		if err := repo.Delete(cmd.Context(), id); err != nil {
			return err
		}
		fmt.Printf("deleted #%d\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doneCmd, rmCmd)
}
