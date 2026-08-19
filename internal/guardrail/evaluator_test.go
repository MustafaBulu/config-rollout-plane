package guardrail

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestCompareOperators(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		operator  Operator
		threshold float64
		want      bool
	}{
		{name: "less than", value: 1, operator: OperatorLessThan, threshold: 2, want: true},
		{name: "less than or equal", value: 2, operator: OperatorLessThanOrEqual, threshold: 2, want: true},
		{name: "greater than", value: 3, operator: OperatorGreaterThan, threshold: 2, want: true},
		{name: "greater than or equal", value: 2, operator: OperatorGreaterThanOrEqual, threshold: 2, want: true},
		{name: "equal", value: 2, operator: OperatorEqual, threshold: 2, want: true},
		{name: "not equal", value: 3, operator: OperatorNotEqual, threshold: 2, want: true},
		{name: "failed comparison", value: 3, operator: OperatorLessThan, threshold: 2, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(tt.value, tt.operator, tt.threshold)
			if got != tt.want {
				t.Fatalf("Compare()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateRequiresConsecutiveFailuresBeforeUnhealthy(t *testing.T) {
	rule := Rule{
		Name:                "error-rate",
		Operator:            OperatorLessThan,
		Threshold:           0.02,
		ConsecutiveFailures: 2,
	}
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	first, err := Evaluate(rule, 0.03, 0, now)
	if err != nil {
		t.Fatalf("first evaluate: %v", err)
	}
	if first.Status != StatusHealthy {
		t.Fatalf("first violating sample should remain healthy, got %s", first.Status)
	}
	if first.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", first.ConsecutiveFailures)
	}

	second, err := Evaluate(rule, 0.04, first.ConsecutiveFailures, now.Add(time.Second))
	if err != nil {
		t.Fatalf("second evaluate: %v", err)
	}
	if second.Status != StatusUnhealthy {
		t.Fatalf("second consecutive violation should be unhealthy, got %s", second.Status)
	}
}

func TestEvaluateResetsConsecutiveFailuresWhenHealthy(t *testing.T) {
	rule := Rule{
		Name:                "error-rate",
		Operator:            OperatorLessThan,
		Threshold:           0.02,
		ConsecutiveFailures: 2,
	}

	got, err := Evaluate(rule, 0.01, 1, time.Now())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got.Status != StatusHealthy {
		t.Fatalf("expected healthy, got %s", got.Status)
	}
	if got.ConsecutiveFailures != 0 {
		t.Fatalf("expected reset consecutive failures, got %d", got.ConsecutiveFailures)
	}
}

func TestEvaluateQueryErrorIsUnknownAndPreservesFailures(t *testing.T) {
	rule := Rule{
		Name:                "error-rate",
		Operator:            OperatorLessThan,
		Threshold:           0.02,
		ConsecutiveFailures: 2,
	}

	got, err := EvaluateQueryResult(rule, 0, errors.New("prometheus unavailable"), 1, time.Now())
	if err != nil {
		t.Fatalf("evaluate query error: %v", err)
	}
	if got.Status != StatusUnknown {
		t.Fatalf("expected unknown, got %s", got.Status)
	}
	if got.ConsecutiveFailures != 1 {
		t.Fatalf("expected preserved failures, got %d", got.ConsecutiveFailures)
	}
}

func TestEvaluateNonFiniteSampleIsUnknown(t *testing.T) {
	rule := Rule{Name: "latency", Operator: OperatorLessThan, Threshold: 100}

	got, err := Evaluate(rule, math.NaN(), 0, time.Now())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got.Status != StatusUnknown {
		t.Fatalf("expected unknown for NaN, got %s", got.Status)
	}
}

func TestAggregateStatus(t *testing.T) {
	if got := AggregateStatus([]Evaluation{{Status: StatusHealthy}, {Status: StatusUnknown}}); got != StatusUnknown {
		t.Fatalf("expected unknown aggregate, got %s", got)
	}
	if got := AggregateStatus([]Evaluation{{Status: StatusUnknown}, {Status: StatusUnhealthy}}); got != StatusUnhealthy {
		t.Fatalf("expected unhealthy aggregate, got %s", got)
	}
}

func TestRuleValidation(t *testing.T) {
	_, err := Evaluate(Rule{Name: "bad", Operator: "contains"}, 1, 0, time.Now())
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}
