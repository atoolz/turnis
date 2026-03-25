package escalation

import (
	"time"

	"github.com/atoolz/turnis/internal/schedule"
)

// WhosOnCallFromEngine wraps schedule.WhosOnCall for use within the escalation engine,
// converting engine-local types to schedule package types.
func WhosOnCallFromEngine(s *schedule.Schedule, overrides []schedule.Override, at time.Time) string {
	return schedule.WhosOnCall(s, overrides, at)
}

func toScheduleModel(s Schedule) *schedule.Schedule {
	sched := &schedule.Schedule{
		ID:             s.ID,
		Timezone:       s.Timezone,
		RotationType:   schedule.RotationType(s.RotationType),
		RotationLength: s.RotationLength,
		HandoffTime:    s.HandoffTime,
		HandoffDay:     s.HandoffDay,
		CreatedAt:      s.CreatedAt,
	}

	for _, l := range s.Layers {
		layer := schedule.Layer{Priority: l.Priority}
		for _, p := range l.Participants {
			layer.Participants = append(layer.Participants, schedule.Participant{UserID: p.UserID})
		}
		sched.Layers = append(sched.Layers, layer)
	}

	return sched
}

func toOverrideModels(overrides []Override) []schedule.Override {
	result := make([]schedule.Override, len(overrides))
	for i, o := range overrides {
		result[i] = schedule.Override{
			UserID:    o.UserID,
			StartTime: o.StartTime,
			EndTime:   o.EndTime,
		}
	}
	return result
}
