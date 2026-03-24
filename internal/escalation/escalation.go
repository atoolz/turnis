package escalation

import "time"

type Policy struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TeamID    string    `json:"team_id"`
	Repeat    int       `json:"repeat"`
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Step struct {
	ID               string `json:"id"`
	PolicyID         string `json:"policy_id"`
	StepOrder        int    `json:"step_order"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	NotifyScheduleID string `json:"notify_schedule_id,omitempty"`
	NotifyUserID     string `json:"notify_user_id,omitempty"`
	NotifyChannel    string `json:"notify_channel"`
}

// NextStep returns the step to execute given a current step index.
// If all steps have been exhausted, it wraps based on the repeat count.
// Returns nil when escalation is fully exhausted.
func NextStep(policy *Policy, currentStep int, currentRepeat int) *Step {
	if len(policy.Steps) == 0 {
		return nil
	}

	next := currentStep + 1
	if next < len(policy.Steps) {
		s := policy.Steps[next]
		return &s
	}

	if currentRepeat+1 < policy.Repeat {
		s := policy.Steps[0]
		return &s
	}

	return nil
}

// StepTimeout returns the duration before escalating to the next step.
func StepTimeout(step *Step) time.Duration {
	if step.TimeoutSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(step.TimeoutSeconds) * time.Second
}
