package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

// Opsgenie API response structures.

type opsgenieResponse[T any] struct {
	Data T `json:"data"`
}

type opsgenieTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type opsgenieUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	FullName string `json:"fullName"`
	Role     struct {
		Name string `json:"name"`
	} `json:"role"`
}

type opsgenieSchedule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	OwnerTeam struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"ownerTeam"`
	Rotations []opsgenieRotation `json:"rotations"`
}

type opsgenieRotation struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Type         string               `json:"type"`
	Length       int                   `json:"length"`
	Participants []opsgenieParticipant `json:"participants"`
}

type opsgenieParticipant struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Username string `json:"username"`
}

type opsgenieEscalation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerTeam struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"ownerTeam"`
	Rules []opsgenieEscalationRule `json:"rules"`
}

type opsgenieEscalationRule struct {
	Condition  string `json:"condition"`
	NotifyType string `json:"notifyType"`
	Delay      struct {
		TimeAmount int `json:"timeAmount"`
	} `json:"delay"`
	Recipient struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"recipient"`
}

type opsgenieIntegration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	OwnerTeam struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"ownerTeam"`
}

type opsgenieClient struct {
	baseURL string
	apiKey  string
	httpCli *http.Client
}

func newOpsgenieClient(apiKey, region string) *opsgenieClient {
	baseURL := "https://api.opsgenie.com"
	if region == "eu" {
		baseURL = "https://api.eu.opsgenie.com"
	}
	return &opsgenieClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpCli: &http.Client{},
	}
}

func (c *opsgenieClient) get(path string, result any) error {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "GenieKey "+c.apiKey)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}
	return nil
}

func migrateOpsgenieCmd() *cobra.Command {
	var apiKey string
	var region string
	var dbDSN string

	cmd := &cobra.Command{
		Use:   "opsgenie",
		Short: "Import data from Opsgenie via REST API",
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiKey == "" {
				return fmt.Errorf("--api-key is required")
			}
			return runOpsgenieMigration(apiKey, region, dbDSN)
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "Opsgenie API key")
	cmd.Flags().StringVar(&region, "region", "us", "Opsgenie region (us or eu)")
	cmd.Flags().StringVar(&dbDSN, "db", "turnis.db", "SQLite database path")

	return cmd
}

func runOpsgenieMigration(apiKey, region, dbDSN string) error {
	client := newOpsgenieClient(apiKey, region)

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

	teamMap := make(map[string]string)
	userMap := make(map[string]string)
	scheduleMap := make(map[string]string)
	policyMap := make(map[string]string)

	// 1. Fetch and import teams
	fmt.Println("Fetching teams from Opsgenie...")
	var teamsResp opsgenieResponse[[]opsgenieTeam]
	if err := client.get("/v2/teams", &teamsResp); err != nil {
		return fmt.Errorf("fetching teams: %w", err)
	}

	for _, ot := range teamsResp.Data {
		team, err := db.CreateTeam(ctx, ot.Name, "")
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create team %q: %v", ot.Name, err))
			continue
		}
		teamMap[ot.ID] = team.ID
		report.teamsCreated++
		fmt.Printf("  Team %q -> %s\n", ot.Name, team.ID)
	}

	// 2. Fetch and import users
	fmt.Println("Fetching users from Opsgenie...")
	var usersResp opsgenieResponse[[]opsgenieUser]
	if err := client.get("/v2/users", &usersResp); err != nil {
		return fmt.Errorf("fetching users: %w", err)
	}

	for _, ou := range usersResp.Data {
		name := ou.FullName
		if name == "" {
			name = ou.Username
		}
		email := ou.Username // Opsgenie uses email as username
		user, err := db.CreateUser(ctx, name, email, "", "", "", "")
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create user %q: %v", name, err))
			continue
		}
		userMap[ou.ID] = user.ID
		// Also map by username so we can resolve schedule participants.
		if existing, ok := userMap[ou.Username]; ok && existing != user.ID {
			report.warnings = append(report.warnings, fmt.Sprintf("username %q maps to multiple users; participant lookup by username may be ambiguous", ou.Username))
		}
		userMap[ou.Username] = user.ID
		report.usersCreated++
		fmt.Printf("  User %q -> %s\n", name, user.ID)
	}

	// 3. Fetch and import schedules
	fmt.Println("Fetching schedules from Opsgenie...")
	var schedulesResp opsgenieResponse[[]opsgenieSchedule]
	if err := client.get("/v2/schedules", &schedulesResp); err != nil {
		return fmt.Errorf("fetching schedules: %w", err)
	}

	for _, ogSched := range schedulesResp.Data {
		newTeamID := teamMap[ogSched.OwnerTeam.ID]
		if newTeamID == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping schedule %q: team %q not found in mapping", ogSched.Name, ogSched.OwnerTeam.Name))
			report.schedulesSkipped++
			continue
		}

		// Fetch full schedule details (includes rotations)
		var schedDetail opsgenieResponse[opsgenieSchedule]
		if err := client.get("/v2/schedules/"+ogSched.ID, &schedDetail); err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to fetch schedule details for %q: %v", ogSched.Name, err))
			report.schedulesSkipped++
			continue
		}

		detail := schedDetail.Data
		tz := detail.Timezone
		if tz == "" {
			tz = "UTC"
		}

		rotationType, rotationLength := mapOpsgenieRotation(detail.Rotations)

		sched := &schedule.Schedule{
			Name:           detail.Name,
			TeamID:         newTeamID,
			Timezone:       tz,
			RotationType:   rotationType,
			RotationLength: rotationLength,
		}

		// Map each Opsgenie rotation to a Turnis layer
		var layers []schedule.Layer
		for priority, rot := range detail.Rotations {
			var participants []schedule.Participant
			for pos, p := range rot.Participants {
				uid := resolveOpsgenieParticipant(p, userMap)
				if uid == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("schedule %q rotation %q: participant %s not found", detail.Name, rot.Name, p.ID))
					continue
				}
				participants = append(participants, schedule.Participant{
					UserID:   uid,
					Position: pos,
				})
			}
			if len(participants) > 0 {
				layers = append(layers, schedule.Layer{
					Priority:     priority,
					Participants: participants,
				})
			}
		}
		sched.Layers = layers

		created, err := db.CreateSchedule(ctx, sched)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create schedule %q: %v", detail.Name, err))
			continue
		}
		scheduleMap[ogSched.ID] = created.ID
		report.schedulesCreated++
		fmt.Printf("  Schedule %q -> %s (%d layers)\n", detail.Name, created.ID, len(layers))
	}

	// 4. Fetch and import escalations
	fmt.Println("Fetching escalations from Opsgenie...")
	var escalationsResp opsgenieResponse[[]opsgenieEscalation]
	if err := client.get("/v2/escalations", &escalationsResp); err != nil {
		return fmt.Errorf("fetching escalations: %w", err)
	}

	for _, oe := range escalationsResp.Data {
		newTeamID := teamMap[oe.OwnerTeam.ID]
		if newTeamID == "" {
			report.warnings = append(report.warnings, fmt.Sprintf("skipping escalation %q: team %q not found", oe.Name, oe.OwnerTeam.Name))
			continue
		}

		policy := &escalation.Policy{
			Name:   oe.Name,
			TeamID: newTeamID,
			Repeat: 1,
		}

		for i, rule := range oe.Rules {
			step := escalation.Step{
				StepOrder:      i,
				TimeoutSeconds: rule.Delay.TimeAmount * 60, // Opsgenie delay is in minutes
				NotifyChannel:  "slack",
			}
			if step.TimeoutSeconds == 0 {
				step.TimeoutSeconds = 300
			}

			switch rule.Recipient.Type {
			case "schedule":
				newScheduleID := scheduleMap[rule.Recipient.ID]
				if newScheduleID == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: schedule %q not found", oe.Name, i, rule.Recipient.Name))
					continue
				}
				step.NotifyScheduleID = newScheduleID
			case "user":
				newUserID := userMap[rule.Recipient.ID]
				if newUserID == "" {
					report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: user %q not found", oe.Name, i, rule.Recipient.Name))
					continue
				}
				step.NotifyUserID = newUserID
			default:
				report.warnings = append(report.warnings, fmt.Sprintf("escalation %q step %d: unsupported recipient type %q, skipping", oe.Name, i, rule.Recipient.Type))
				continue
			}

			policy.Steps = append(policy.Steps, step)
		}

		created, err := db.CreatePolicy(ctx, policy)
		if err != nil {
			report.warnings = append(report.warnings, fmt.Sprintf("failed to create policy %q: %v", oe.Name, err))
			continue
		}
		policyMap[oe.ID] = created.ID
		report.policiesCreated++
		fmt.Printf("  Policy %q -> %s (%d steps)\n", oe.Name, created.ID, len(policy.Steps))
	}

	// 5. Fetch and import integrations
	fmt.Println("Fetching integrations from Opsgenie...")
	var integrationsResp opsgenieResponse[[]opsgenieIntegration]
	if err := client.get("/v2/integrations", &integrationsResp); err != nil {
		report.warnings = append(report.warnings, fmt.Sprintf("failed to fetch integrations: %v", err))
	} else {
		for _, oi := range integrationsResp.Data {
			newTeamID := teamMap[oi.OwnerTeam.ID]
			if newTeamID == "" {
				report.warnings = append(report.warnings, fmt.Sprintf("skipping integration %q: team not found", oi.Name))
				continue
			}

			// Opsgenie integrations don't directly link to escalations, leave policy empty
			created, err := db.CreateIntegration(ctx, oi.Name, newTeamID, "webhook", "")
			if err != nil {
				report.warnings = append(report.warnings, fmt.Sprintf("failed to create integration %q: %v", oi.Name, err))
				continue
			}
			report.integrationsCreated++
			fmt.Printf("  Integration %q -> %s\n", oi.Name, created.ID)
		}
	}

	report.print()
	return nil
}

func mapOpsgenieRotation(rotations []opsgenieRotation) (schedule.RotationType, int) {
	if len(rotations) == 0 {
		return schedule.RotationWeekly, 1
	}
	rot := rotations[0]
	length := rot.Length
	if length == 0 {
		length = 1
	}
	switch rot.Type {
	case "daily":
		return schedule.RotationDaily, length
	case "hourly":
		return schedule.RotationCustom, length
	default:
		return schedule.RotationWeekly, length
	}
}

func resolveOpsgenieParticipant(p opsgenieParticipant, userMap map[string]string) string {
	if uid := userMap[p.ID]; uid != "" {
		return uid
	}
	if uid := userMap[p.Username]; uid != "" {
		return uid
	}
	return ""
}
