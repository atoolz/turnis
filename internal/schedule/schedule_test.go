package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func makeSchedule(created time.Time, participants []string) *Schedule {
	var parts []Participant
	for i, uid := range participants {
		parts = append(parts, Participant{
			ID:       uid + "-p",
			LayerID:  "layer-1",
			UserID:   uid,
			Position: i,
		})
	}

	return &Schedule{
		ID:             "sched-1",
		Name:           "primary",
		TeamID:         "team-1",
		Timezone:       "UTC",
		RotationType:   RotationWeekly,
		RotationLength: 1,
		HandoffTime:    "09:00",
		HandoffDay:     "monday",
		Layers: []Layer{
			{
				ID:           "layer-1",
				ScheduleID:   "sched-1",
				Priority:     1,
				Participants: parts,
			},
		},
		CreatedAt: created,
		UpdatedAt: created,
	}
}

func TestWhosOnCall_WeeklyRotation(t *testing.T) {
	t.Helper()

	// Schedule created on Monday 2025-01-06 at 08:00 UTC.
	// First handoff: 2025-01-06 09:00 UTC (Monday).
	// With 3 participants rotating weekly:
	//   week 0 (Jan 6-12): alice
	//   week 1 (Jan 13-19): bob
	//   week 2 (Jan 20-26): carol
	//   week 3 (Jan 27-Feb 2): alice (wrap-around)
	created := time.Date(2025, 1, 6, 8, 0, 0, 0, time.UTC)
	s := makeSchedule(created, []string{"alice", "bob", "carol"})

	tests := []struct {
		name   string
		at     time.Time
		expect string
	}{
		{
			name:   "before first handoff returns first participant",
			at:     time.Date(2025, 1, 6, 8, 30, 0, 0, time.UTC),
			expect: "alice",
		},
		{
			name:   "week 0 after handoff",
			at:     time.Date(2025, 1, 7, 12, 0, 0, 0, time.UTC),
			expect: "alice",
		},
		{
			name:   "week 1 returns bob",
			at:     time.Date(2025, 1, 14, 12, 0, 0, 0, time.UTC),
			expect: "bob",
		},
		{
			name:   "week 2 returns carol",
			at:     time.Date(2025, 1, 21, 12, 0, 0, 0, time.UTC),
			expect: "carol",
		},
		{
			name:   "week 3 wraps to alice",
			at:     time.Date(2025, 1, 28, 12, 0, 0, 0, time.UTC),
			expect: "alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhosOnCall(s, nil, tt.at)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestWhosOnCall_OverridePriority(t *testing.T) {
	created := time.Date(2025, 1, 6, 8, 0, 0, 0, time.UTC)
	s := makeSchedule(created, []string{"alice", "bob"})

	overrides := []Override{
		{
			ID:         "ovr-1",
			ScheduleID: "sched-1",
			UserID:     "dave",
			StartTime:  time.Date(2025, 1, 7, 0, 0, 0, 0, time.UTC),
			EndTime:    time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC),
		},
	}

	at := time.Date(2025, 1, 7, 12, 0, 0, 0, time.UTC)
	got := WhosOnCall(s, overrides, at)
	assert.Equal(t, "dave", got, "override should take priority over rotation")
}

func TestWhosOnCall_OverrideBoundary(t *testing.T) {
	created := time.Date(2025, 1, 6, 8, 0, 0, 0, time.UTC)
	s := makeSchedule(created, []string{"alice", "bob"})

	start := time.Date(2025, 1, 7, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 7, 18, 0, 0, 0, time.UTC)

	overrides := []Override{
		{
			ID:        "ovr-1",
			UserID:    "dave",
			StartTime: start,
			EndTime:   end,
		},
	}

	tests := []struct {
		name   string
		at     time.Time
		expect string
	}{
		{
			name:   "exact start time is included",
			at:     start,
			expect: "dave",
		},
		{
			name:   "exact end time is excluded",
			at:     end,
			expect: "alice",
		},
		{
			name:   "one second before end is included",
			at:     end.Add(-time.Second),
			expect: "dave",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhosOnCall(s, overrides, tt.at)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestWhosOnCall_EmptyLayers(t *testing.T) {
	s := &Schedule{
		ID:             "sched-1",
		Timezone:       "UTC",
		RotationType:   RotationWeekly,
		RotationLength: 1,
		HandoffTime:    "09:00",
		HandoffDay:     "monday",
		Layers:         nil,
		CreatedAt:      time.Now(),
	}

	got := WhosOnCall(s, nil, time.Now())
	assert.Equal(t, "", got)
}

func TestWhosOnCall_EmptyParticipants(t *testing.T) {
	s := &Schedule{
		ID:             "sched-1",
		Timezone:       "UTC",
		RotationType:   RotationWeekly,
		RotationLength: 1,
		HandoffTime:    "09:00",
		HandoffDay:     "monday",
		Layers: []Layer{
			{
				ID:           "layer-1",
				Priority:     1,
				Participants: nil,
			},
		},
		CreatedAt: time.Now(),
	}

	got := WhosOnCall(s, nil, time.Now())
	assert.Equal(t, "", got)
}

func TestWhosOnCall_SingleParticipant(t *testing.T) {
	created := time.Date(2025, 1, 6, 8, 0, 0, 0, time.UTC)
	s := makeSchedule(created, []string{"alice"})

	tests := []struct {
		name string
		at   time.Time
	}{
		{"week 0", time.Date(2025, 1, 7, 12, 0, 0, 0, time.UTC)},
		{"week 5", time.Date(2025, 2, 11, 12, 0, 0, 0, time.UTC)},
		{"week 52", time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WhosOnCall(s, nil, tt.at)
			assert.Equal(t, "alice", got)
		})
	}
}

func TestWhosOnCall_RotationWrapAround(t *testing.T) {
	created := time.Date(2025, 1, 6, 8, 0, 0, 0, time.UTC)
	s := makeSchedule(created, []string{"alice", "bob"})

	// 10 weeks later, rotation index 10 % 2 == 0 => alice
	at := time.Date(2025, 3, 17, 12, 0, 0, 0, time.UTC)
	got := WhosOnCall(s, nil, at)
	assert.Equal(t, "alice", got)

	// 11 weeks later, rotation index 11 % 2 == 1 => bob
	at = time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	got = WhosOnCall(s, nil, at)
	assert.Equal(t, "bob", got)
}
