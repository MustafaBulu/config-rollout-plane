package guardrail

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrNoData         = errors.New("no data")
	ErrMultipleSeries = errors.New("multiple series")
)

type Status string

const (
	StatusHealthy   Status = "HEALTHY"
	StatusUnhealthy Status = "UNHEALTHY"
	StatusUnknown   Status = "UNKNOWN"
)

type Operator string

const (
	OperatorLessThan           Operator = "<"
	OperatorLessThanOrEqual    Operator = "<="
	OperatorGreaterThan        Operator = ">"
	OperatorGreaterThanOrEqual Operator = ">="
	OperatorEqual              Operator = "=="
	OperatorNotEqual           Operator = "!="
)

type Rule struct {
	Name                string
	Query               string
	Operator            Operator
	Threshold           float64
	ConsecutiveFailures int
}

type Evaluation struct {
	Name                        string
	Query                       string
	Status                      Status
	Value                       float64
	Threshold                   float64
	Operator                    Operator
	ConsecutiveFailures         int
	RequiredConsecutiveFailures int
	Reason                      string
	ObservedAt                  time.Time
}

func (r Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("%w: guardrail name is required", ErrInvalidInput)
	}
	if !validOperator(r.Operator) {
		return fmt.Errorf("%w: unsupported guardrail operator %q", ErrInvalidInput, r.Operator)
	}
	if r.ConsecutiveFailures < 0 {
		return fmt.Errorf("%w: consecutive failures cannot be negative", ErrInvalidInput)
	}
	return nil
}

func (r Rule) requiredFailures() int {
	if r.ConsecutiveFailures == 0 {
		return 1
	}
	return r.ConsecutiveFailures
}

func validOperator(operator Operator) bool {
	switch operator {
	case OperatorLessThan,
		OperatorLessThanOrEqual,
		OperatorGreaterThan,
		OperatorGreaterThanOrEqual,
		OperatorEqual,
		OperatorNotEqual:
		return true
	default:
		return false
	}
}
