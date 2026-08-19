package guardrail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Queryer interface {
	Query(ctx context.Context, query string, at time.Time) (float64, error)
}

type PrometheusClient struct {
	baseURL *url.URL
	client  *http.Client
	timeout time.Duration
}

func NewPrometheusClient(baseURL string, timeout time.Duration) (*PrometheusClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("%w: prometheus base url is required", ErrInvalidInput)
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: prometheus base url: %v", ErrInvalidInput, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: prometheus base url must include scheme and host", ErrInvalidInput)
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return &PrometheusClient{
		baseURL: parsed,
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}, nil
}

func (c *PrometheusClient) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	if c == nil {
		return 0, fmt.Errorf("%w: prometheus client is nil", ErrInvalidInput)
	}
	if strings.TrimSpace(query) == "" {
		return 0, fmt.Errorf("%w: prometheus query is required", ErrInvalidInput)
	}

	queryCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/query"
	values := endpoint.Query()
	values.Set("query", query)
	if !at.IsZero() {
		values.Set("time", strconv.FormatFloat(float64(at.UnixNano())/float64(time.Second), 'f', 3, 64))
	}
	endpoint.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(queryCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus query failed with status %d", resp.StatusCode)
	}

	var payload prometheusResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if payload.Status != "success" {
		if payload.Error != "" {
			return 0, fmt.Errorf("prometheus query failed: %s", payload.Error)
		}
		return 0, fmt.Errorf("prometheus query failed")
	}

	return parsePrometheusValue(payload.Data.ResultType, payload.Data.Result)
}

type prometheusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

type prometheusVectorResult struct {
	Value []json.RawMessage `json:"value"`
}

func parsePrometheusValue(resultType string, raw json.RawMessage) (float64, error) {
	switch resultType {
	case "scalar":
		var value []json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
		return parsePrometheusSample(value)
	case "vector":
		var values []prometheusVectorResult
		if err := json.Unmarshal(raw, &values); err != nil {
			return 0, err
		}
		if len(values) == 0 {
			return 0, ErrNoData
		}
		if len(values) > 1 {
			return 0, ErrMultipleSeries
		}
		return parsePrometheusSample(values[0].Value)
	default:
		return 0, fmt.Errorf("unsupported prometheus result type %q", resultType)
	}
}

func parsePrometheusSample(value []json.RawMessage) (float64, error) {
	if len(value) != 2 {
		return 0, fmt.Errorf("invalid prometheus sample")
	}

	var raw string
	if err := json.Unmarshal(value[1], &raw); err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

var _ Queryer = (*PrometheusClient)(nil)
