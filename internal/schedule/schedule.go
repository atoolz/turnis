package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RotationType string

const (
	RotationDaily  RotationType = "daily"
	RotationWeekly RotationType = "weekly"
	RotationCustom RotationType = "custom"
)

type Schedule struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	TeamID         string       `json:"team_id"`
	Timezone       string       `json:"timezone"`
	RotationType   RotationType `json:"rotation_type"`
	RotationLength int          `json:"rotation_length"`
	HandoffTime    string       `json:"handoff_time"`
	HandoffDay     string       `json:"handoff_day"`
	Layers         []Layer      `json:"layers"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type Layer struct {
	ID           string        `json:"id"`
	ScheduleID   string        `json:"schedule_id"`
	Priority     int           `json:"priority"`
	Participants []Participant `json:"participants"`
}

type Participant struct {
	ID       string `json:"id"`
	LayerID  string `json:"layer_id"`
	UserID   string `json:"user_id"`
	Position int    `json:"position"`
}

type Override struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	UserID     string    `json:"user_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// WhosOnCall resolves the current on-call user for a schedule at a given time.
// It checks overrides first (highest priority), then walks layers by priority.
func WhosOnCall(s *Schedule, overrides []Override, at time.Time) string {
	for _, o := range overrides {
		if !at.Before(o.StartTime) && at.Before(o.EndTime) {
			return o.UserID
		}
	}

	if len(s.Layers) == 0 {
		return ""
	}

	layer := s.Layers[0]
	for _, l := range s.Layers {
		if l.Priority > layer.Priority {
			layer = l
		}
	}

	if len(layer.Participants) == 0 {
		return ""
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localAt := at.In(loc)

	idx := rotationIndex(s, localAt, len(layer.Participants))
	return layer.Participants[idx].UserID
}

// firstHandoff computes the first handoff datetime from the schedule's
// HandoffTime and HandoffDay, anchored to the schedule's CreatedAt date.
func firstHandoff(s *Schedule, loc *time.Location) time.Time {
	hour, minute := parseHandoffTime(s.HandoffTime)
	created := s.CreatedAt.UTC().In(loc)

	anchor := time.Date(created.Year(), created.Month(), created.Day(), hour, minute, 0, 0, loc)

	if s.RotationType == RotationWeekly {
		targetDay := parseWeekday(s.HandoffDay)
		for anchor.Weekday() != targetDay {
			anchor = anchor.AddDate(0, 0, 1)
		}
	}

	if anchor.Before(created) {
		switch s.RotationType {
		case RotationDaily:
			anchor = anchor.AddDate(0, 0, 1)
		case RotationWeekly:
			anchor = anchor.AddDate(0, 0, 7)
		default:
			anchor = anchor.AddDate(0, 0, s.RotationLength)
		}
	}

	return anchor
}

// rotationIndex counts how many complete rotation periods have passed
// between the schedule's first handoff and the given time, using calendar
// stepping instead of duration division to avoid DST boundary errors.
func rotationIndex(s *Schedule, at time.Time, count int) int {
	if count == 0 {
		return 0
	}

	loc := at.Location()
	epoch := firstHandoff(s, loc)

	if at.Before(epoch) {
		return 0
	}

	daysPerStep, weeksPerStep := s.RotationLength, 0
	switch s.RotationType {
	case RotationWeekly:
		weeksPerStep = s.RotationLength
		daysPerStep = 0
	case RotationDaily:
		daysPerStep = s.RotationLength
	default:
		daysPerStep = s.RotationLength
	}

	if daysPerStep == 0 && weeksPerStep == 0 {
		return 0
	}

	rotations := 0
	cursor := epoch
	for {
		var next time.Time
		if weeksPerStep > 0 {
			next = cursor.AddDate(0, 0, weeksPerStep*7)
		} else {
			next = cursor.AddDate(0, 0, daysPerStep)
		}
		if next.After(at) {
			break
		}
		cursor = next
		rotations++
	}

	return rotations % count
}

func parseHandoffTime(t string) (int, int) {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 9, 0
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 9, 0
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return h, 0
	}
	return h, m
}

func parseWeekday(day string) time.Weekday {
	days := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	if d, ok := days[strings.ToLower(day)]; ok {
		return d
	}
	return time.Monday
}

// FormatHandoff returns a human-readable handoff description.
func FormatHandoff(s *Schedule) string {
	switch s.RotationType {
	case RotationWeekly:
		return fmt.Sprintf("every %s at %s", s.HandoffDay, s.HandoffTime)
	case RotationDaily:
		return fmt.Sprintf("daily at %s", s.HandoffTime)
	default:
		return fmt.Sprintf("every %d days at %s", s.RotationLength, s.HandoffTime)
	}
}
