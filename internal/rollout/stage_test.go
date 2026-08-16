package rollout

import "testing"

func TestDefaultStagePlan(t *testing.T) {
	stages := DefaultStagePlan()
	if err := ValidateStagePlan(stages); err != nil {
		t.Fatalf("default stage plan should be valid: %v", err)
	}

	got := []int{stages[0].Percentage, stages[1].Percentage, stages[2].Percentage}
	want := []int{5, 25, 100}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage %d percentage = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestValidateStagePlanRejectsNonIncreasingPercentages(t *testing.T) {
	err := ValidateStagePlan([]Stage{
		{ID: "stage-25", Percentage: 25},
		{ID: "stage-5", Percentage: 5},
		{ID: "stage-100", Percentage: 100},
	})
	if err == nil {
		t.Fatal("expected non-increasing stage plan to fail")
	}
}

func TestValidateStagePlanRequiresFinalHundredPercent(t *testing.T) {
	err := ValidateStagePlan([]Stage{
		{ID: "stage-5", Percentage: 5},
		{ID: "stage-25", Percentage: 25},
	})
	if err == nil {
		t.Fatal("expected missing 100 percent final stage to fail")
	}
}
