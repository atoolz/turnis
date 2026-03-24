package alert

import "time"

type Status string

const (
	StatusFiring       Status = "firing"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Alert struct {
	ID             string    `json:"id"`
	IntegrationID  string    `json:"integration_id"`
	Fingerprint    string    `json:"fingerprint,omitempty"`
	Status         Status    `json:"status"`
	Title          string    `json:"title"`
	Message        string    `json:"message,omitempty"`
	Severity       Severity  `json:"severity"`
	Source         string    `json:"source,omitempty"`
	AcknowledgedBy string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type IncomingAlert struct {
	Title       string            `json:"title"`
	Message     string            `json:"message,omitempty"`
	Severity    Severity          `json:"severity,omitempty"`
	Source      string            `json:"source,omitempty"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// Deduplicate checks if an alert with the same fingerprint already exists
// and is still active (firing or acknowledged).
func Deduplicate(existing []Alert, incoming IncomingAlert) *Alert {
	if incoming.Fingerprint == "" {
		return nil
	}

	for i := range existing {
		if existing[i].Fingerprint == incoming.Fingerprint &&
			existing[i].Status != StatusResolved {
			return &existing[i]
		}
	}

	return nil
}
