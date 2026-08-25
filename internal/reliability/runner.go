package reliability

import (
	"context"
	"time"
)

type Step struct {
	Name string
	Run  func(context.Context, *Harness, *ScenarioResult) error
}

type Runner struct {
	Name  string
	Steps []Step
}

func (r Runner) Run(ctx context.Context, harness *Harness) (ScenarioResult, error) {
	result := ScenarioResult{
		Name:      r.Name,
		StartedAt: time.Now().UTC(),
		Passed:    true,
	}
	started := time.Now()

	for _, step := range r.Steps {
		eventStarted := time.Now()
		err := step.Run(ctx, harness, &result)
		event := ScenarioEvent{
			Name:     step.Name,
			Passed:   err == nil,
			Duration: time.Since(eventStarted),
		}
		if err != nil {
			event.Error = err.Error()
			result.Passed = false
			result.Events = append(result.Events, event)
			result.Duration = time.Since(started)
			return result, err
		}
		result.Events = append(result.Events, event)
	}

	result.Duration = time.Since(started)
	return result, nil
}
