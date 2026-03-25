package escalation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePolicy(repeat int, steps []Step) *Policy {
	return &Policy{
		ID:     "pol-1",
		Name:   "test-policy",
		TeamID: "team-1",
		Repeat: repeat,
		Steps:  steps,
	}
}

func TestNextStep_FirstStep(t *testing.T) {
	p := makePolicy(1, []Step{
		{ID: "s1", StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
		{ID: "s2", StepOrder: 1, TimeoutSeconds: 600, NotifyChannel: "sms"},
	})

	result := NextStep(p, -1, 0)

	require.NotNil(t, result.Step)
	assert.Equal(t, "s1", result.Step.ID)
	assert.Equal(t, 0, result.NewStepIdx)
	assert.Equal(t, 0, result.NewRepeat)
}

func TestNextStep_SecondStep(t *testing.T) {
	p := makePolicy(1, []Step{
		{ID: "s1", StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
		{ID: "s2", StepOrder: 1, TimeoutSeconds: 600, NotifyChannel: "sms"},
	})

	result := NextStep(p, 0, 0)

	require.NotNil(t, result.Step)
	assert.Equal(t, "s2", result.Step.ID)
	assert.Equal(t, 1, result.NewStepIdx)
	assert.Equal(t, 0, result.NewRepeat)
}

func TestNextStep_WrapsOnRepeat(t *testing.T) {
	p := makePolicy(2, []Step{
		{ID: "s1", StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
		{ID: "s2", StepOrder: 1, TimeoutSeconds: 600, NotifyChannel: "sms"},
	})

	// After last step of first repeat, should wrap to step 0 with repeat incremented.
	result := NextStep(p, 1, 0)

	require.NotNil(t, result.Step)
	assert.Equal(t, "s1", result.Step.ID)
	assert.Equal(t, 0, result.NewStepIdx)
	assert.Equal(t, 1, result.NewRepeat)
}

func TestNextStep_ExhaustedReturnsNil(t *testing.T) {
	p := makePolicy(1, []Step{
		{ID: "s1", StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
	})

	// Only one step, one repeat. After step 0, escalation is exhausted.
	result := NextStep(p, 0, 0)

	assert.Nil(t, result.Step)
	assert.Equal(t, 0, result.NewStepIdx)
	assert.Equal(t, 0, result.NewRepeat)
}

func TestNextStep_EmptySteps(t *testing.T) {
	p := makePolicy(1, nil)

	result := NextStep(p, -1, 0)

	assert.Nil(t, result.Step)
}

func TestNextStep_SingleStepWithRepeat(t *testing.T) {
	p := makePolicy(2, []Step{
		{ID: "s1", StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
	})

	// First call: start at -1
	r1 := NextStep(p, -1, 0)
	require.NotNil(t, r1.Step)
	assert.Equal(t, "s1", r1.Step.ID)
	assert.Equal(t, 0, r1.NewStepIdx)
	assert.Equal(t, 0, r1.NewRepeat)

	// Second call: after step 0, repeat 0 -> wrap to step 0, repeat 1
	r2 := NextStep(p, r1.NewStepIdx, r1.NewRepeat)
	require.NotNil(t, r2.Step)
	assert.Equal(t, "s1", r2.Step.ID)
	assert.Equal(t, 0, r2.NewStepIdx)
	assert.Equal(t, 1, r2.NewRepeat)

	// Third call: after step 0, repeat 1 -> exhausted
	r3 := NextStep(p, r2.NewStepIdx, r2.NewRepeat)
	assert.Nil(t, r3.Step)
}

func TestNextStep_CursorValues(t *testing.T) {
	p := makePolicy(3, []Step{
		{ID: "s1", StepOrder: 0},
		{ID: "s2", StepOrder: 1},
		{ID: "s3", StepOrder: 2},
	})

	tests := []struct {
		name          string
		currentStep   int
		currentRepeat int
		wantStepID    string
		wantStepIdx   int
		wantRepeat    int
		wantNil       bool
	}{
		{"start", -1, 0, "s1", 0, 0, false},
		{"step 0 to 1", 0, 0, "s2", 1, 0, false},
		{"step 1 to 2", 1, 0, "s3", 2, 0, false},
		{"wrap repeat 0 to 1", 2, 0, "s1", 0, 1, false},
		{"repeat 1 step 0 to 1", 0, 1, "s2", 1, 1, false},
		{"repeat 1 step 1 to 2", 1, 1, "s3", 2, 1, false},
		{"wrap repeat 1 to 2", 2, 1, "s1", 0, 2, false},
		{"repeat 2 last step exhausts", 2, 2, "", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NextStep(p, tt.currentStep, tt.currentRepeat)
			if tt.wantNil {
				assert.Nil(t, result.Step)
			} else {
				require.NotNil(t, result.Step)
				assert.Equal(t, tt.wantStepID, result.Step.ID)
				assert.Equal(t, tt.wantStepIdx, result.NewStepIdx)
				assert.Equal(t, tt.wantRepeat, result.NewRepeat)
			}
		})
	}
}
