package reliability

import "time"

type ScenarioResult struct {
	Name      string          `json:"name"`
	Passed    bool            `json:"passed"`
	StartedAt time.Time       `json:"started_at"`
	Duration  time.Duration   `json:"duration"`
	Events    []ScenarioEvent `json:"events"`
	Metrics   map[string]int  `json:"metrics,omitempty"`
}

type ScenarioEvent struct {
	Name     string        `json:"name"`
	Passed   bool          `json:"passed"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

func (r *ScenarioResult) addMetric(name string, value int) {
	if r.Metrics == nil {
		r.Metrics = map[string]int{}
	}
	r.Metrics[name] = value
}
