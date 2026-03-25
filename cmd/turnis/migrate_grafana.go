package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

// Grafana OnCall export JSON structures.

type grafanaExport struct {
	Teams            []grafanaTeam            `json:"teams"`
	Users            []grafanaUser            `json:"users"`
	Schedules        []grafanaSchedule        `json:"schedules"`
	EscalationChains []grafanaEscalationChain `json:"escalation_chains"`
	Integrations     []grafanaIntegration     `json:"integrations"`
}

type grafanaTeam struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SlackChannel string `json:"slack_channel"`
}

type grafanaUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	SlackID  string `json:"slack_id"`
}

type grafanaSchedule struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	TeamID   string         `json:"team_id"`
	Timezone string         `json:"timezone"`
	Type     string         `json:"type"`
	Shifts   []grafanaShift `json:"shifts"`
}

type grafanaShift struct {
	UserIDs       []string `json:"user_ids"`
	RotationStart string   `json:"rotation_start"`
	Frequency     string   `json:"frequency"`
	Interval      int      `json:"interval"`
}

type grafanaEscalationChain struct {
	ID     string                  `json:"id"`
	Name   string                  `json:"name"`
	TeamID string                  `json:"team_id"`
	Steps  []grafanaEscalationStep `json:"steps"`
}

type grafanaEscalationStep struct {
	Type          string `json:"type"`
	TargetID      string `json:"target_id"`
	Duration      int    `json:"duration"`
	NotifyChannel string `json:"notify_channel"`
}

type grafanaIntegration struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	TeamID            string `json:"team_id"`
	Type              string `json:"type"`
	EscalationChainID string `json:"escalation_chain_id"`
}

type migrationReport struct {
	teamsCreated        int
	usersCreated        int
	schedulesCreated    int
	schedulesSkipped    int
	policiesCreated     int
	integrationsCreated int
	warnings            []string
}

func (r *migrationReport) print() {
	fmt.Println("\n=== Migration Report ===")
	fmt.Printf("Teams created:        %d\n", r.teamsCreated)
	fmt.Printf("Users created:        %d\n", r.usersCreated)
	fmt.Printf("Schedules created:    %d\n", r.schedulesCreated)
	fmt.Printf("Schedules skipped:    %d\n", r.schedulesSkipped)
	fmt.Printf("Policies created:     %d\n", r.policiesCreated)
	fmt.Printf("Integrations created: %d\n", r.integrationsCreated)
	if len(r.warnings) > 0 {
		fmt.Println("\nWarnings:")
		for _, w := range r.warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
	fmt.Println()
}

func migrateGrafanaOnCallCmd() *cobra.Command {
	var inputFile string
	var dbDSN string

	cmd := &cobra.Command{
		Use:   "grafana-oncall",
		Short: "Import data from a Grafana OnCall JSON export",
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputFile == "" {
				return fmt.Errorf("--input is required")
			}
			return runGrafanaMigration(inputFile, dbDSN)
		},
	}

	cmd.Flags().StringVar(&inputFile, "input", "", "path to Grafana OnCall export JSON file")
	cmd.Flags().StringVar(&dbDSN, "db", "turnis.db", "SQLite database path")

	return cmd
}

func runGrafanaMigration(inputFile, dbDSN string) error {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}

	var export grafanaExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	db, err := store.New(config.DatabaseConfig{Driver: "sqlite", DSN: dbDSN})
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	ctx := context.Background()
	report := &migrationReport{}

	// ID mappings: old Grafana ID -> new Turnis ID
	teamMap := make(map[string]string)
	userMap := make(map[string]string)
	scheduleMap := make(map[string]string)
	policyMap := make(map[string]string)

	// 1. Import teams
	fmt.Println("Importing teams...")
	for _, gt := range export.Teams {
		team, err := db.CreateTeam(ctx, gt.Name, gt.SlackChannel)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create team %q: %v", gt.Name, err))
			continue
		}
		teamMap[gt.ID] = team.ID
		report.teamsCreated++
		fmt.Printf("  Team %q -> %s\n", gt.Name, team.ID)
	}

	// 2. Import users
	fmt.Println("Importing users...")
	for _, gu := range export.Users {
		name := gu.Username
		if name == "" {
			name = gu.Email
		}
		if gu.Email == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping user %q: no email address", name))
			continue
		}
		// Users in Grafana OnCall are not team-scoped, so leave team_id empty.
		user, err := db.CreateUser(ctx, name, gu.Email, gu.Phone, gu.SlackID, "", "")
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create user %q: %v", name, err))
			continue
		}
		userMap[gu.ID] = user.ID
		report.usersCreated++
		fmt.Printf("  User %q -> %s\n", name, user.ID)
	}

	// 3. Import schedules
	fmt.Println("Importing schedules...")
	for _, gs := range export.Schedules {
		if gs.Type == "ical" {
			report.schedulesSkipped++
			report.warnings = append(report.warnings, fmt.Sprintf("skipping iCal schedule %q (unsupported)", gs.Name))
			continue
		}

		newTeamID := teamMap[gs.TeamID]
		if newTeamID == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping schedule %q: team %s not found in mapping", gs.Name, gs.TeamID))
			report.schedulesSkipped++
			continue
		}

		rotationType := mapGrafanaFrequency(gs.Shifts)
		rotationLength := 1
		if len(gs.Shifts) > 0 && gs.Shifts[0].Interval > 0 {
			rotationLength = gs.Shifts[0].Interval
		}

		sched := &schedule.Schedule{
			Name:           gs.Name,
			TeamID:         newTeamID,
			Timezone:       gs.Timezone,
			RotationType:   rotationType,
			RotationLength: rotationLength,
		}

		// Build a single layer with all participants from all shifts
		var participants []schedule.Participant
		position := 0
		for _, shift := range gs.Shifts {
			for _, uid := range shift.UserIDs {
				newUID := userMap[uid]
				if newUID == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("schedule %q: user %s not found in mapping, skipping participant", gs.Name, uid))
					continue
				}
				participants = append(participants, schedule.Participant{
					UserID:   newUID,
					Position: position,
				})
				position++
			}
		}

		if len(participants) > 0 {
			sched.Layers = []schedule.Layer{
				{Priority: 0, Participants: participants},
			}
		}

		created, err := db.CreateSchedule(ctx, sched)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create schedule %q: %v", gs.Name, err))
			continue
		}
		scheduleMap[gs.ID] = created.ID
		report.schedulesCreated++
		fmt.Printf("  Schedule %q -> %s (%d participants)\n", gs.Name, created.ID, len(participants))
	}

	// 4. Import escalation chains as policies
	fmt.Println("Importing escalation policies...")
	for _, gc := range export.EscalationChains {
		newTeamID := teamMap[gc.TeamID]
		if newTeamID == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping escalation chain %q: team %s not found", gc.Name, gc.TeamID))
			continue
		}

		policy := &escalation.Policy{
			Name:   gc.Name,
			TeamID: newTeamID,
			Repeat: 1,
		}

		for i, gs := range gc.Steps {
			step := escalation.Step{
				StepOrder:      i,
				TimeoutSeconds: gs.Duration,
				NotifyChannel:  gs.NotifyChannel,
			}
			if step.NotifyChannel == "" {
				step.NotifyChannel = "slack"
			}
			if step.TimeoutSeconds == 0 {
				step.TimeoutSeconds = 300
			}

			switch gs.Type {
			case "notify_schedule":
				newScheduleID := scheduleMap[gs.TargetID]
				if newScheduleID == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: schedule %s not found", gc.Name, i, gs.TargetID))
					continue
				}
				step.NotifyScheduleID = newScheduleID
			case "notify_user":
				newUserID := userMap[gs.TargetID]
				if newUserID == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: user %s not found", gc.Name, i, gs.TargetID))
					continue
				}
				step.NotifyUserID = newUserID
			default:
				report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: unsupported type %q, skipping step", gc.Name, i, gs.Type))
				continue
			}

			policy.Steps = append(policy.Steps, step)
		}

		created, err := db.CreatePolicy(ctx, policy)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create policy %q: %v", gc.Name, err))
			continue
		}
		policyMap[gc.ID] = created.ID
		report.policiesCreated++
		fmt.Printf("  Policy %q -> %s (%d steps)\n", gc.Name, created.ID, len(policy.Steps))
	}

	// 5. Import integrations
	fmt.Println("Importing integrations...")
	for _, gi := range export.Integrations {
		newTeamID := teamMap[gi.TeamID]
		if newTeamID == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping integration %q: team %s not found", gi.Name, gi.TeamID))
			continue
		}

		policyID := ""
		if gi.EscalationChainID != "" {
			policyID = policyMap[gi.EscalationChainID]
			if policyID == "" {
				report.warnings = append(report.warnings, fmt.Sprintf("integration %q: escalation chain %s not found in mapping", gi.Name, gi.EscalationChainID))
			}
		}

		intType := gi.Type
		if intType == "" {
			intType = "webhook"
		}

		created, err := db.CreateIntegration(ctx, gi.Name, newTeamID, intType, policyID)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create integration %q: %v", gi.Name, err))
			continue
		}
		report.integrationsCreated++
		fmt.Printf("  Integration %q -> %s\n", gi.Name, created.ID)
	}

	report.print()
	return nil
}

func mapGrafanaFrequency(shifts []grafanaShift) schedule.RotationType {
	if len(shifts) == 0 {
		return schedule.RotationWeekly
	}
	switch shifts[0].Frequency {
	case "daily":
		return schedule.RotationDaily
	case "hourly":
		return schedule.RotationCustom
	default:
		return schedule.RotationWeekly
	}
}
