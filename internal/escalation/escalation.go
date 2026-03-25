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

// StepResult holds the next step to execute along with the updated cursor state.
// The caller should store NewStepIdx and NewRepeat as the new current position.
type StepResult struct {
	Step      *Step
	NewStepIdx int
	NewRepeat  int
}

// NextStep returns the next step to execute given a current step index and repeat count.
// Returns nil Step when escalation is fully exhausted.
// The caller MUST use NewStepIdx and NewRepeat to update cursor state,
// rather than reimplementing the index arithmetic.
func NextStep(policy *Policy, currentStep int, currentRepeat int) StepResult {
	if len(policy.Steps) == 0 {
		return StepResult{}
	}

	next := currentStep + 1
	if next < len(policy.Steps) {
		s := policy.Steps[next]
		return StepResult{Step: &s, NewStepIdx: next, NewRepeat: currentRepeat}
	}

	if currentRepeat+1 < policy.Repeat {
		s := policy.Steps[0]
		return StepResult{Step: &s, NewStepIdx: 0, NewRepeat: currentRepeat + 1}
	}

	return StepResult{}
}

// StepTimeout returns the duration before escalating to the next step.
func StepTimeout(step *Step) time.Duration {
	if step.TimeoutSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(step.TimeoutSeconds) * time.Second
}
