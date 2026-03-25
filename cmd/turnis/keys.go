package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/store"
)

func keysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys",
	}

	cmd.AddCommand(keysCreateCmd())
	cmd.AddCommand(keysListCmd())
	cmd.AddCommand(keysDeleteCmd())
	return cmd
}

func keysCreateCmd() *cobra.Command {
	var (
		configPath string
		name       string
		teamID     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			db, err := store.New(cfg.Database)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			if err := db.Migrate(); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}

			// Generate a random 32-byte key.
			rawKey := make([]byte, 32)
			if _, err := rand.Read(rawKey); err != nil {
				return fmt.Errorf("generating random key: %w", err)
			}
			plaintext := hex.EncodeToString(rawKey)

			// Store SHA-256 hash only.
			h := sha256.Sum256([]byte(plaintext))
			hash := hex.EncodeToString(h[:])

			ctx := cmd.Context()
			apiKey, err := db.CreateAPIKey(ctx, hash, name, teamID)
			if err != nil {
				return fmt.Errorf("creating api key: %w", err)
			}

			// Record audit event.
			if auditErr := db.RecordAudit(ctx, "", "api_key.created", "api_key", apiKey.ID, map[string]string{
				"name": name,
			}); auditErr != nil {
				slog.Error("failed to record audit for api key creation", "error", auditErr)
			}

			fmt.Println("API key created successfully.")
			fmt.Println()
			fmt.Printf("  ID:   %s\n", apiKey.ID)
			fmt.Printf("  Name: %s\n", apiKey.Name)
			fmt.Printf("  Key:  %s\n", plaintext)
			fmt.Println()
			fmt.Println("Save this key now. It cannot be retrieved later.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "turnis.yaml", "path to config file")
	cmd.Flags().StringVar(&name, "name", "", "name for the API key")
	cmd.Flags().StringVar(&teamID, "team-id", "", "optional team ID to associate with the key")

	return cmd
}

func keysListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			db, err := store.New(cfg.Database)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			if err := db.Migrate(); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}

			keys, err := db.ListAPIKeys(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing api keys: %w", err)
			}

			if len(keys) == 0 {
				fmt.Println("No API keys found.")
				return nil
			}

			fmt.Printf("%-36s  %-20s  %-36s  %-20s  %-20s\n", "ID", "NAME", "TEAM ID", "CREATED", "LAST USED")
			for _, k := range keys {
				lastUsed := "never"
				if k.LastUsedAt != nil {
					lastUsed = k.LastUsedAt.Format("2006-01-02 15:04")
				}
				teamID := ""
				if k.TeamID != "" {
					teamID = k.TeamID
				}
				fmt.Printf("%-36s  %-20s  %-36s  %-20s  %-20s\n",
					k.ID, k.Name, teamID, k.CreatedAt.Format("2006-01-02 15:04"), lastUsed)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "turnis.yaml", "path to config file")
	return cmd
}

func keysDeleteCmd() *cobra.Command {
	var (
		configPath string
		id         string
	)

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id is required")
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			db, err := store.New(cfg.Database)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			if err := db.Migrate(); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}

			ctx := cmd.Context()
			if err := db.DeleteAPIKey(ctx, id); err != nil {
				return fmt.Errorf("deleting api key: %w", err)
			}

			// Record audit event.
			if auditErr := db.RecordAudit(ctx, "", "api_key.deleted", "api_key", id, nil); auditErr != nil {
				slog.Error("failed to record audit for api key deletion", "error", auditErr)
			}

			fmt.Printf("API key %s deleted.\n", id)
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "turnis.yaml", "path to config file")
	cmd.Flags().StringVar(&id, "id", "", "ID of the API key to delete")

	return cmd
}
