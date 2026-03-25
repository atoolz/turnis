package alert

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeduplicate(t *testing.T) {
	now := time.Now().UTC()

	firingAlert := Alert{
		ID:            "a1",
		IntegrationID: "int-1",
		Fingerprint:   "fp-abc",
		Status:        StatusFiring,
		Title:         "CPU High",
		Severity:      SeverityWarning,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	ackedAlert := Alert{
		ID:            "a2",
		IntegrationID: "int-1",
		Fingerprint:   "fp-abc",
		Status:        StatusAcknowledged,
		Title:         "CPU High (acked)",
		Severity:      SeverityWarning,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	resolvedAlert := Alert{
		ID:            "a3",
		IntegrationID: "int-1",
		Fingerprint:   "fp-abc",
		Status:        StatusResolved,
		Title:         "CPU High (resolved)",
		Severity:      SeverityWarning,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	tests := []struct {
		name     string
		existing []Alert
		incoming IncomingAlert
		wantID   string
		wantNil  bool
	}{
		{
			name:     "matches active alert by fingerprint",
			existing: []Alert{firingAlert},
			incoming: IncomingAlert{Title: "CPU High", Fingerprint: "fp-abc"},
			wantID:   "a1",
			wantNil:  false,
		},
		{
			name:     "matches acknowledged alert by fingerprint",
			existing: []Alert{ackedAlert},
			incoming: IncomingAlert{Title: "CPU High", Fingerprint: "fp-abc"},
			wantID:   "a2",
			wantNil:  false,
		},
		{
			name:     "does not match resolved alert",
			existing: []Alert{resolvedAlert},
			incoming: IncomingAlert{Title: "CPU High", Fingerprint: "fp-abc"},
			wantNil:  true,
		},
		{
			name:     "does not match different fingerprint",
			existing: []Alert{firingAlert},
			incoming: IncomingAlert{Title: "Disk Full", Fingerprint: "fp-xyz"},
			wantNil:  true,
		},
		{
			name:     "empty fingerprint returns nil",
			existing: []Alert{firingAlert},
			incoming: IncomingAlert{Title: "CPU High", Fingerprint: ""},
			wantNil:  true,
		},
		{
			name:     "empty existing list returns nil",
			existing: nil,
			incoming: IncomingAlert{Title: "CPU High", Fingerprint: "fp-abc"},
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Deduplicate(tt.existing, tt.incoming)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.wantID, got.ID)
			}
		})
	}
}
