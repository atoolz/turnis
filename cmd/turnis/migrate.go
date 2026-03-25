package main

import "github.com/spf13/cobra"

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate from another on-call tool into Turnis",
	}

	cmd.AddCommand(migrateGrafanaOnCallCmd())
	cmd.AddCommand(migrateOpsgenieCmd())

	return cmd
}
