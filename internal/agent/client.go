package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"config-rollout-plane/internal/dataplane"
)

type SnapshotClient struct {
	BaseURL    string
	AgentID    string
	Token      string
	HTTPClient *http.Client
}

type FetchResult struct {
	Snapshot dataplane.Snapshot
	ETag     string
	Changed  bool
}

func (c SnapshotClient) Fetch(ctx context.Context, etag string) (FetchResult, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/agents/" + c.AgentID + "/snapshot"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return FetchResult{ETag: etag, Changed: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return FetchResult{}, readErr
		}
		return FetchResult{}, fmt.Errorf("snapshot request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var snapshot dataplane.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return FetchResult{}, err
	}
	return FetchResult{Snapshot: snapshot, ETag: resp.Header.Get("ETag"), Changed: true}, nil
}

type Acknowledger interface {
	Acknowledge(ctx context.Context, agentID string, token string, item dataplane.SnapshotItem, revision int64) error
}

type RegistrationClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

type RegisterInput struct {
	BootstrapToken string            `json:"bootstrap_token"`
	ID             string            `json:"id,omitempty"`
	Service        string            `json:"service"`
	Environment    string            `json:"environment"`
	Zone           string            `json:"zone,omitempty"`
	Instance       string            `json:"instance"`
	Labels         map[string]string `json:"labels,omitempty"`
}

type RegisterResult struct {
	AgentID            string
	InstanceCredential string
}

func (c RegistrationClient) Register(ctx context.Context, input RegisterInput) (RegisterResult, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	body, err := json.Marshal(input)
	if err != nil {
		return RegisterResult{}, err
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/agents/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return RegisterResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return RegisterResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return RegisterResult{}, readErr
		}
		return RegisterResult{}, fmt.Errorf("agent registration failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var payload registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{
		AgentID:            payload.Agent.ID,
		InstanceCredential: payload.InstanceCredential,
	}, nil
}

type registerResponse struct {
	Agent struct {
		ID string `json:"id"`
	} `json:"agent"`
	InstanceCredential string `json:"instance_credential"`
}

type ControlPlaneAcknowledger struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (a ControlPlaneAcknowledger) Acknowledge(ctx context.Context, agentID string, token string, item dataplane.SnapshotItem, revision int64) error {
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	payload := map[string]any{
		"config_definition_id": item.ConfigDefinitionID,
		"version_id":           item.VersionID,
		"snapshot_revision":    revision,
	}
	if item.Assignment.RolloutID != "" && item.Assignment.StageID != "" {
		payload["rollout_id"] = item.Assignment.RolloutID
		payload["stage_id"] = item.Assignment.StageID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := strings.TrimRight(a.BaseURL, "/") + "/v1/agents/" + agentID + "/acknowledgements"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("acknowledgement failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	return nil
}
