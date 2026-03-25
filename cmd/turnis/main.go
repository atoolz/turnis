package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "turnis",
		Short: "On-call, not on-everything",
		Long:  "Turnis is an open-source, Slack-native on-call management tool. Single binary, BYOT Twilio, ntfy push.",
	}

	root.AddCommand(serveCmd())
	root.AddCommand(versionCmd())
	root.AddCommand(migrateCmd())
	root.AddCommand(keysCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Turnis server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			return runServer(cfg)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "turnis.yaml", "path to config file")

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("turnis %s\n", version)
		},
	}
}
