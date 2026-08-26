package reliability

import (
	"context"
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
	"config-rollout-plane/internal/guardrail"
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
	Concurrency int
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
	concurrency int

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
	if options.Concurrency == 0 {
		options.Concurrency = 16
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
		concurrency: options.Concurrency,
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

func (h *Harness) RegisterAgents(ctx context.Context, count int, concurrency int, prefix string) ([]AgentIdentity, error) {
	if count < 1 {
		return nil, errors.New("agent count must be positive")
	}
	if prefix == "" {
		prefix = "agent"
	}
	agents := make([]AgentIdentity, count)
	err := runParallel(count, concurrency, func(index int) error {
		identity, err := h.RegisterAgent(ctx, fmt.Sprintf("%s-%04d", prefix, index+1))
		if err != nil {
			return err
		}
		agents[index] = identity
		return nil
	})
	return agents, err
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

func (h *Harness) CreateRollout(ctx context.Context, input rollout.CreateRolloutInput) (rollout.Rollout, []rollout.StageTarget, error) {
	if input.TenantID == "" {
		input.TenantID = "payments"
	}
	if input.Key == "" {
		input.Key = paymentFailureRateKey
	}
	if input.Environment == "" {
		input.Environment = h.environment
	}
	if input.CandidateVersionNumber == 0 {
		input.CandidateVersionNumber = 2
	}
	if len(input.TargetServices) == 0 {
		input.TargetServices = []string{h.service}
	}
	if input.RequiredAckPercentage == 0 {
		input.RequiredAckPercentage = 100
	}
	if input.DeploymentTimeout == 0 {
		input.DeploymentTimeout = 5 * time.Minute
	}
	return h.rollouts.CreateRollout(ctx, input)
}

func (h *Harness) GetRollout(ctx context.Context, rolloutID string) (rollout.Rollout, []rollout.StageTarget, rollout.Coverage, error) {
	return h.rollouts.GetRollout(ctx, rolloutID)
}

func (h *Harness) ReconcileActive(ctx context.Context) error {
	return h.rollouts.ReconcileActive(ctx)
}

func (h *Harness) SetGuardrailQueryer(queryer guardrail.Queryer) {
	h.rollouts.SetGuardrailQueryer(queryer)
}

func (h *Harness) FetchSnapshot(ctx context.Context, identity AgentIdentity) (dataplane.Snapshot, error) {
	baseURL, err := h.DataPlaneURL()
	if err != nil {
		return dataplane.Snapshot{}, err
	}
	client := agent.SnapshotClient{
		BaseURL: baseURL,
		AgentID: identity.ID,
		Token:   identity.Token,
	}
	result, err := client.Fetch(ctx, "")
	if err != nil {
		return dataplane.Snapshot{}, err
	}
	return result.Snapshot, nil
}

func (h *Harness) AcknowledgeSnapshotItem(ctx context.Context, identity AgentIdentity, item dataplane.SnapshotItem, revision int64) error {
	baseURL, err := h.ControlPlaneURL()
	if err != nil {
		return err
	}
	acknowledger := agent.ControlPlaneAcknowledger{BaseURL: baseURL}
	return acknowledger.Acknowledge(ctx, identity.ID, identity.Token, item, revision)
}

func (h *Harness) AcknowledgeAssignedStage(ctx context.Context, identities []AgentIdentity, stageID string, concurrency int) (int, error) {
	var mu sync.Mutex
	acknowledged := 0
	err := runParallel(len(identities), concurrency, func(index int) error {
		identity := identities[index]
		snapshot, err := h.FetchSnapshot(ctx, identity)
		if err != nil {
			return err
		}
		for _, item := range snapshot.Configs {
			if item.Assignment.StageID != stageID {
				continue
			}
			if err := h.AcknowledgeSnapshotItem(ctx, identity, item, snapshot.Revision); err != nil {
				return err
			}
			mu.Lock()
			acknowledged++
			mu.Unlock()
		}
		return nil
	})
	return acknowledged, err
}

func runParallel(count int, concurrency int, fn func(index int) error) error {
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan int)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := fn(index); err != nil {
					select {
					case errs <- err:
					default:
					}
				}
			}
		}()
	}
	for index := 0; index < count; index++ {
		select {
		case err := <-errs:
			close(jobs)
			wg.Wait()
			return err
		case jobs <- index:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}
