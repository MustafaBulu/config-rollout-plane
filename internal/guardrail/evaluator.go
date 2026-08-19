package guardrail

import (
	"fmt"
	"math"
	"time"
)

func Evaluate(rule Rule, value float64, previousFailures int, observedAt time.Time) (Evaluation, error) {
	if err := rule.Validate(); err != nil {
		return Evaluation{}, err
	}
	if previousFailures < 0 {
		return Evaluation{}, fmt.Errorf("%w: previous failures cannot be negative", ErrInvalidInput)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Unknown(rule, previousFailures, "guardrail sample is not finite", observedAt)
	}

	passed := Compare(value, rule.Operator, rule.Threshold)
	required := rule.requiredFailures()
	result := Evaluation{
		Name:                        rule.Name,
		Query:                       rule.Query,
		Status:                      StatusHealthy,
		Value:                       value,
		Threshold:                   rule.Threshold,
		Operator:                    rule.Operator,
		RequiredConsecutiveFailures: required,
		ObservedAt:                  observedAt,
	}

	if passed {
		return result, nil
	}

	result.ConsecutiveFailures = previousFailures + 1
	if result.ConsecutiveFailures >= required {
		result.Status = StatusUnhealthy
		result.Reason = "guardrail threshold violated"
		return result, nil
	}

	result.Reason = "guardrail threshold violated below consecutive failure threshold"
	return result, nil
}

func Unknown(rule Rule, previousFailures int, reason string, observedAt time.Time) (Evaluation, error) {
	if err := rule.Validate(); err != nil {
		return Evaluation{}, err
	}
	if previousFailures < 0 {
		return Evaluation{}, fmt.Errorf("%w: previous failures cannot be negative", ErrInvalidInput)
	}
	if reason == "" {
		reason = "guardrail status is unknown"
	}

	return Evaluation{
		Name:                        rule.Name,
		Query:                       rule.Query,
		Status:                      StatusUnknown,
		Threshold:                   rule.Threshold,
		Operator:                    rule.Operator,
		ConsecutiveFailures:         previousFailures,
		RequiredConsecutiveFailures: rule.requiredFailures(),
		Reason:                      reason,
		ObservedAt:                  observedAt,
	}, nil
}

func EvaluateQueryResult(rule Rule, value float64, queryErr error, previousFailures int, observedAt time.Time) (Evaluation, error) {
	if queryErr != nil {
		return Unknown(rule, previousFailures, queryErr.Error(), observedAt)
	}
	return Evaluate(rule, value, previousFailures, observedAt)
}

func Compare(value float64, operator Operator, threshold float64) bool {
	switch operator {
	case OperatorLessThan:
		return value < threshold
	case OperatorLessThanOrEqual:
		return value <= threshold
	case OperatorGreaterThan:
		return value > threshold
	case OperatorGreaterThanOrEqual:
		return value >= threshold
	case OperatorEqual:
		return value == threshold
	case OperatorNotEqual:
		return value != threshold
	default:
		return false
	}
}

func AggregateStatus(evaluations []Evaluation) Status {
	status := StatusHealthy
	for _, evaluation := range evaluations {
		if evaluation.Status == StatusUnhealthy {
			return StatusUnhealthy
		}
		if evaluation.Status == StatusUnknown {
			status = StatusUnknown
		}
	}
	return status
}
