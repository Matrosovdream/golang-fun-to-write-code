package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"taskcli/internal/storage"
)

var repo storage.Repository

var rootCmd = &cobra.Command{
	Use:           "taskcli",
	Short:         "A terminal task manager",
	SilenceUsage:  true, // don't dump usage on every runtime error
	SilenceErrors: true, // we print the error once, in Execute
	PersistentPreRunE: func(*cobra.Command, []string) error {
		var err error
		repo, err = storage.New(storage.Config{
			Backend: viper.GetString("backend"),
			Path:    viper.GetString("path"),
		})
		return err
	},
	PersistentPostRunE: func(*cobra.Command, []string) error {
		if repo != nil {
			return repo.Close()
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("backend", "sqlite", "storage backend: sqlite or json")
	rootCmd.PersistentFlags().String("path", defaultPath(), "path to the data file")

	// Effective priority: flag > env (TASKCLI_BACKEND, TASKCLI_PATH) >
	// config file (~/.taskcli.yaml) > flag default.
	viper.SetEnvPrefix("taskcli")
	viper.AutomaticEnv()
	_ = viper.BindPFlag("backend", rootCmd.PersistentFlags().Lookup("backend"))
	_ = viper.BindPFlag("path", rootCmd.PersistentFlags().Lookup("path"))

	viper.SetConfigName(".taskcli")
	viper.SetConfigType("yaml")
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(home)
	}
	_ = viper.ReadInConfig() // the config file is optional
}

func defaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "tasks.db"
	}
	return filepath.Join(home, ".taskcli.db")
}
