package rollout

import (
	"fmt"
	"time"
)

type Stage struct {
	ID              string
	Percentage      int
	MinimumDuration time.Duration
}

func DefaultStagePlan() []Stage {
	return []Stage{
		{ID: "stage-5", Percentage: 5, MinimumDuration: time.Minute},
		{ID: "stage-25", Percentage: 25, MinimumDuration: 2 * time.Minute},
		{ID: "stage-100", Percentage: 100, MinimumDuration: 3 * time.Minute},
	}
}

func ValidateStagePlan(stages []Stage) error {
	if len(stages) == 0 {
		return fmt.Errorf("stage plan is required")
	}

	previous := 0
	for _, stage := range stages {
		if stage.ID == "" {
			return fmt.Errorf("stage id is required")
		}
		if stage.Percentage <= 0 || stage.Percentage > 100 {
			return fmt.Errorf("invalid stage percentage: %d", stage.Percentage)
		}
		if stage.Percentage <= previous {
			return fmt.Errorf("stage percentages must increase")
		}
		previous = stage.Percentage
	}

	if stages[len(stages)-1].Percentage != 100 {
		return fmt.Errorf("final stage must be 100 percent")
	}
	return nil
}
