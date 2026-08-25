package reliability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"time"

	"config-rollout-plane/internal/agent"
	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/controlplane"
	"config-rollout-plane/internal/dataplane"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/health"
	"config-rollout-plane/internal/rollout"
)

const bootstrapToken = "reliability-bootstrap"

type FailureTarget string

const (
	FailureControlPlane FailureTarget = "control-plane"
	FailureDataPlane    FailureTarget = "data-plane"
)

type Options struct {
	Service     string
	Environment domain.Environment
	TempDir     string
}

type AgentIdentity struct {
	ID    string
	Token string
}

type Harness struct {
	registry  *configregistry.Service
	agents    *agentregistry.Service
	rollouts  *rollout.Service
	snapshots *dataplane.DynamicSnapshotStore

	service     string
	environment domain.Environment
	tempDir     string

	mu           sync.Mutex
	controlPlane *httptest.Server
	dataPlane    *httptest.Server
}

func NewHarness(ctx context.Context, options Options) (*Harness, error) {
	if options.Service == "" {
		options.Service = "payment-service"
	}
	if options.Environment == "" {
		options.Environment = domain.EnvironmentProduction
	}
	if options.TempDir == "" {
		options.TempDir = "."
	}

	registryStore := configregistry.NewMemoryStore()
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	if err := registryStore.CreateTenant(ctx, domain.Tenant{
		ID:        "payments",
		Name:      "Payments",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	if err := registryStore.CreateDefinition(ctx, domain.ConfigDefinition{
		ID:           "cfg_payment_failure_rate",
		TenantID:     "payments",
		Key:          "payment.failure_rate",
		Description:  "Synthetic payment authorization failure rate used by reliability scenarios.",
		Schema:       []byte(`{"type":"number","minimum":0,"maximum":1}`),
		DefaultValue: []byte(`0`),
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return nil, err
	}
	v1, err := registryStore.CreateVersion(ctx, domain.ConfigVersion{
		ID:                 "ver_payment_failure_rate_v1",
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Value:              []byte(`0`),
		CreatedBy:          "reliability",
		CreatedAt:          now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := registryStore.CreateVersion(ctx, domain.ConfigVersion{
		ID:                 "ver_payment_failure_rate_v2",
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Value:              []byte(`0.2`),
		CreatedBy:          "reliability",
		CreatedAt:          now,
	}); err != nil {
		return nil, err
	}
	if err := registryStore.SaveEnvironmentState(ctx, domain.ConfigEnvironmentState{
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Environment:        options.Environment,
		StableVersionID:    v1.ID,
		UpdatedAt:          now,
	}); err != nil {
		return nil, err
	}

	registry := configregistry.NewService(registryStore, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(agentregistry.NewMemoryStore(), bootstrapToken, time.Hour)
	rollouts := rollout.NewService(rollout.NewMemoryStore(), registry, agents)
	snapshots := dataplane.NewDynamicSnapshotStore(registry, agents, rollouts, []string{"payments"})

	return &Harness{
		registry:    registry,
		agents:      agents,
		rollouts:    rollouts,
		snapshots:   snapshots,
		service:     options.Service,
		environment: options.Environment,
		tempDir:     options.TempDir,
	}, nil
}

func (h *Harness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.controlPlane != nil {
		h.controlPlane.Close()
		h.controlPlane = nil
	}
	if h.dataPlane != nil {
		h.dataPlane.Close()
		h.dataPlane = nil
	}
}

func (h *Harness) StartControlPlane() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.controlPlane != nil {
		return
	}
	h.controlPlane = httptest.NewServer(controlplane.NewHandlerWithServices(
		h.registry,
		h.agents,
		h.rollouts,
		health.StaticChecker{},
	))
}

func (h *Harness) StartDataPlane() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dataPlane != nil {
		return
	}
	h.dataPlane = httptest.NewServer(dataplane.NewHandler(
		h.snapshots,
		dataplane.NewAgentCredentialVerifier(h.agents),
	))
}

func (h *Harness) InjectFailure(target FailureTarget) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch target {
	case FailureControlPlane:
		if h.controlPlane == nil {
			return nil
		}
		h.controlPlane.Close()
		h.controlPlane = nil
	case FailureDataPlane:
		if h.dataPlane == nil {
			return nil
		}
		h.dataPlane.Close()
		h.dataPlane = nil
	default:
		return fmt.Errorf("unknown failure target: %s", target)
	}
	return nil
}

func (h *Harness) Recover(target FailureTarget) error {
	switch target {
	case FailureControlPlane:
		h.StartControlPlane()
	case FailureDataPlane:
		h.StartDataPlane()
	default:
		return fmt.Errorf("unknown failure target: %s", target)
	}
	return nil
}

func (h *Harness) Restart(target FailureTarget) error {
	if err := h.InjectFailure(target); err != nil {
		return err
	}
	return h.Recover(target)
}

func (h *Harness) ControlPlaneURL() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.controlPlane == nil {
		return "", errors.New("control plane is not running")
	}
	return h.controlPlane.URL, nil
}

func (h *Harness) DataPlaneURL() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.dataPlane == nil {
		return "", errors.New("data plane is not running")
	}
	return h.dataPlane.URL, nil
}

func (h *Harness) RegisterAgent(ctx context.Context, id string) (AgentIdentity, error) {
	baseURL, err := h.ControlPlaneURL()
	if err != nil {
		return AgentIdentity{}, err
	}
	client := agent.RegistrationClient{BaseURL: baseURL}
	result, err := client.Register(ctx, agent.RegisterInput{
		BootstrapToken: bootstrapToken,
		ID:             id,
		Service:        h.service,
		Environment:    string(h.environment),
		Zone:           "zone-1",
		Instance:       id,
	})
	if err != nil {
		return AgentIdentity{}, err
	}
	return AgentIdentity{ID: result.AgentID, Token: result.InstanceCredential}, nil
}

func (h *Harness) Heartbeat(ctx context.Context, identity AgentIdentity) error {
	baseURL, err := h.ControlPlaneURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/agents/"+identity.ID+"/heartbeat", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+identity.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("heartbeat failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	return nil
}

func (h *Harness) NewSyncer(identity AgentIdentity) (*agent.Syncer, *agent.SnapshotCache, error) {
	baseURL, err := h.DataPlaneURL()
	if err != nil {
		return nil, nil, err
	}
	cache := agent.NewSnapshotCache(filepath.Join(h.tempDir, identity.ID+"-snapshot.json"))
	syncer := &agent.Syncer{
		Client: agent.SnapshotClient{
			BaseURL: baseURL,
			AgentID: identity.ID,
			Token:   identity.Token,
		},
		Cache: cache,
	}
	return syncer, cache, nil
}

func (h *Harness) CachedConfigValue(cache *agent.SnapshotCache, key string) (string, int, error) {
	item, err := cache.Config(key)
	if err != nil {
		return "", 0, err
	}
	return string(item.Value), item.Version, nil
}

func (h *Harness) CreateRollout(ctx context.Context) (string, error) {
	payload := map[string]any{
		"tenant_id":                  "payments",
		"key":                        "payment.failure_rate",
		"environment":                string(h.environment),
		"candidate_version":          2,
		"target_services":            []string{h.service},
		"required_ack_percentage":    100,
		"deployment_timeout_seconds": 300,
	}
	baseURL, err := h.ControlPlaneURL()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/rollouts", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("create rollout failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	return response.ID, nil
}
