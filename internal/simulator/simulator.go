package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/dataplane"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/rollout"
)

type Options struct {
	AgentCount  int
	Concurrency int
	Service     string
	Environment domain.Environment
}

type Result struct {
	AgentCount         int           `json:"agent_count"`
	RegisteredAgents   int           `json:"registered_agents"`
	SnapshotReads      int           `json:"snapshot_reads"`
	Acknowledgements   int           `json:"acknowledgements"`
	AgentsPerSecond    float64       `json:"agents_per_second"`
	SnapshotsPerSecond float64       `json:"snapshots_per_second"`
	AcksPerSecond      float64       `json:"acks_per_second"`
	RolloutID          string        `json:"rollout_id"`
	FinalState         string        `json:"final_state"`
	FinalStage         string        `json:"final_stage"`
	Duration           time.Duration `json:"duration"`
	RegisterLatency    Latency       `json:"register_latency"`
	SnapshotLatency    Latency       `json:"snapshot_latency"`
	AckLatency         Latency       `json:"ack_latency"`
	StageResults       []StageResult `json:"stage_results"`
}

type StageResult struct {
	StageID   string        `json:"stage_id"`
	Targets   int           `json:"targets"`
	Acked     int           `json:"acked"`
	Coverage  float64       `json:"coverage"`
	Duration  time.Duration `json:"duration"`
	NextState string        `json:"next_state"`
	NextStage string        `json:"next_stage,omitempty"`
}

type Latency struct {
	Count int           `json:"count"`
	Min   time.Duration `json:"min"`
	Avg   time.Duration `json:"avg"`
	P50   time.Duration `json:"p50"`
	P95   time.Duration `json:"p95"`
	Max   time.Duration `json:"max"`
}

type virtualAgent struct {
	ID    string
	Token string
}

type environment struct {
	registry  *configregistry.Service
	agents    *agentregistry.Service
	rollouts  *rollout.Service
	snapshots *dataplane.DynamicSnapshotStore
}

func Run(ctx context.Context, options Options) (Result, error) {
	options = normalizeOptions(options)
	started := time.Now()

	env, err := setup(ctx, options)
	if err != nil {
		return Result{}, err
	}

	agents, registerLatencies, err := registerAgents(ctx, env.agents, options)
	if err != nil {
		return Result{}, err
	}

	created, _, err := env.rollouts.CreateRollout(ctx, rollout.CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.failure_rate",
		Environment:            options.Environment,
		CandidateVersionNumber: 2,
		TargetServices:         []string{options.Service},
		Stages: []rollout.Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-25", Percentage: 25},
			{ID: "stage-100", Percentage: 100},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     5 * time.Minute,
	})
	if err != nil {
		return Result{}, err
	}

	var snapshotLatencies []time.Duration
	var ackLatencies []time.Duration
	var stageResults []StageResult
	for {
		active, targets, _, err := env.rollouts.GetRollout(ctx, created.ID)
		if err != nil {
			return Result{}, err
		}
		if active.State.Terminal() {
			break
		}

		stageStarted := time.Now()
		stageSnapshotLatencies, stageAckLatencies, err := acknowledgeStage(ctx, env, agents, active.CurrentStageID, options.Concurrency)
		if err != nil {
			return Result{}, err
		}
		snapshotLatencies = append(snapshotLatencies, stageSnapshotLatencies...)
		ackLatencies = append(ackLatencies, stageAckLatencies...)

		updated, _, coverage, err := env.rollouts.GetRollout(ctx, created.ID)
		if err != nil {
			return Result{}, err
		}
		acked := len(stageAckLatencies)
		coveragePercent := 0.0
		if len(targets) > 0 {
			coveragePercent = float64(acked) / float64(len(targets)) * 100
		}
		stageResults = append(stageResults, StageResult{
			StageID:   active.CurrentStageID,
			Targets:   len(targets),
			Acked:     acked,
			Coverage:  coveragePercent,
			Duration:  time.Since(stageStarted),
			NextState: string(updated.State),
			NextStage: updated.CurrentStageID,
		})
		if updated.State.Terminal() {
			break
		}
		if updated.CurrentStageID == active.CurrentStageID && coverage.Percentage < active.RequiredAckPercent {
			return Result{}, fmt.Errorf("stage %s did not reach required acknowledgement coverage", active.CurrentStageID)
		}
	}

	final, _, _, err := env.rollouts.GetRollout(ctx, created.ID)
	if err != nil {
		return Result{}, err
	}

	duration := time.Since(started)
	return Result{
		AgentCount:         options.AgentCount,
		RegisteredAgents:   len(agents),
		SnapshotReads:      len(snapshotLatencies),
		Acknowledgements:   len(ackLatencies),
		AgentsPerSecond:    perSecond(len(agents), duration),
		SnapshotsPerSecond: perSecond(len(snapshotLatencies), duration),
		AcksPerSecond:      perSecond(len(ackLatencies), duration),
		RolloutID:          created.ID,
		FinalState:         string(final.State),
		FinalStage:         final.CurrentStageID,
		Duration:           duration,
		RegisterLatency:    summarize(registerLatencies),
		SnapshotLatency:    summarize(snapshotLatencies),
		AckLatency:         summarize(ackLatencies),
		StageResults:       stageResults,
	}, nil
}

func perSecond(count int, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(count) / duration.Seconds()
}

func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	return json.Marshal(struct {
		alias
		Duration string `json:"duration"`
	}{
		alias:    alias(r),
		Duration: r.Duration.String(),
	})
}

func normalizeOptions(options Options) Options {
	if options.AgentCount == 0 {
		options.AgentCount = 1000
	}
	if options.Concurrency == 0 {
		options.Concurrency = 64
	}
	if options.Service == "" {
		options.Service = "payment-service"
	}
	if options.Environment == "" {
		options.Environment = domain.EnvironmentProduction
	}
	return options
}

func setup(ctx context.Context, options Options) (environment, error) {
	registryStore := configregistry.NewMemoryStore()
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	if err := registryStore.CreateTenant(ctx, domain.Tenant{
		ID:        "payments",
		Name:      "Payments",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return environment{}, err
	}
	if err := registryStore.CreateDefinition(ctx, domain.ConfigDefinition{
		ID:           "cfg_payment_failure_rate",
		TenantID:     "payments",
		Key:          "payment.failure_rate",
		Description:  "Synthetic payment authorization failure rate used by the simulator.",
		Schema:       []byte(`{"type":"number","minimum":0,"maximum":1}`),
		DefaultValue: []byte(`0`),
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return environment{}, err
	}
	v1, err := registryStore.CreateVersion(ctx, domain.ConfigVersion{
		ID:                 "ver_payment_failure_rate_v1",
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Value:              []byte(`0`),
		CreatedBy:          "simulator",
		CreatedAt:          now,
	})
	if err != nil {
		return environment{}, err
	}
	if _, err := registryStore.CreateVersion(ctx, domain.ConfigVersion{
		ID:                 "ver_payment_failure_rate_v2",
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Value:              []byte(`0.2`),
		CreatedBy:          "simulator",
		CreatedAt:          now,
	}); err != nil {
		return environment{}, err
	}
	if err := registryStore.SaveEnvironmentState(ctx, domain.ConfigEnvironmentState{
		ConfigDefinitionID: "cfg_payment_failure_rate",
		Environment:        options.Environment,
		StableVersionID:    v1.ID,
		UpdatedAt:          now,
	}); err != nil {
		return environment{}, err
	}

	registry := configregistry.NewService(registryStore, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(agentregistry.NewMemoryStore(), "sim-bootstrap", time.Hour)
	rollouts := rollout.NewService(rollout.NewMemoryStore(), registry, agents)
	snapshots := dataplane.NewDynamicSnapshotStore(registry, agents, rollouts, []string{"payments"})
	return environment{registry: registry, agents: agents, rollouts: rollouts, snapshots: snapshots}, nil
}

func registerAgents(ctx context.Context, service *agentregistry.Service, options Options) ([]virtualAgent, []time.Duration, error) {
	agents := make([]virtualAgent, options.AgentCount)
	latencies := make([]time.Duration, options.AgentCount)
	err := parallel(options.AgentCount, options.Concurrency, func(index int) error {
		id := fmt.Sprintf("agent-%04d", index+1)
		started := time.Now()
		result, err := service.Register(ctx, agentregistry.RegisterInput{
			BootstrapToken: "sim-bootstrap",
			ID:             id,
			Service:        options.Service,
			Environment:    options.Environment,
			Zone:           fmt.Sprintf("zone-%d", index%3),
			Instance:       id,
		})
		latencies[index] = time.Since(started)
		if err != nil {
			return err
		}
		agents[index] = virtualAgent{ID: result.Agent.ID, Token: result.Credential.Token}
		return nil
	})
	return agents, latencies, err
}

type pendingAck struct {
	AgentID            string
	Token              string
	ConfigDefinitionID string
	VersionID          string
	SnapshotRevision   int64
	RolloutID          string
	StageID            string
}

func acknowledgeStage(ctx context.Context, env environment, agents []virtualAgent, stageID string, concurrency int) ([]time.Duration, []time.Duration, error) {
	snapshotLatencies := make([]time.Duration, len(agents))
	pending := make([]pendingAck, 0)
	var pendingMu sync.Mutex
	err := parallel(len(agents), concurrency, func(index int) error {
		agent := agents[index]
		started := time.Now()
		snapshot, err := env.snapshots.GetSnapshot(ctx, agent.ID)
		snapshotLatencies[index] = time.Since(started)
		if err != nil {
			return err
		}
		for _, item := range snapshot.Configs {
			if item.Assignment.RolloutID == "" || item.Assignment.StageID != stageID {
				continue
			}
			pendingMu.Lock()
			pending = append(pending, pendingAck{
				AgentID:            agent.ID,
				Token:              agent.Token,
				ConfigDefinitionID: item.ConfigDefinitionID,
				VersionID:          item.VersionID,
				SnapshotRevision:   snapshot.Revision,
				RolloutID:          item.Assignment.RolloutID,
				StageID:            item.Assignment.StageID,
			})
			pendingMu.Unlock()
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	ackLatencies := make([]time.Duration, len(pending))
	for index, ack := range pending {
		started := time.Now()
		if _, err := env.agents.Acknowledge(ctx, agentregistry.AcknowledgeInput{
			AgentID:            ack.AgentID,
			Token:              ack.Token,
			ConfigDefinitionID: ack.ConfigDefinitionID,
			VersionID:          ack.VersionID,
			SnapshotRevision:   ack.SnapshotRevision,
		}); err != nil {
			return nil, nil, err
		}
		if _, err := env.rollouts.Acknowledge(ctx, rollout.AcknowledgeInput{
			RolloutID:        ack.RolloutID,
			StageID:          ack.StageID,
			AgentID:          ack.AgentID,
			VersionID:        ack.VersionID,
			SnapshotRevision: ack.SnapshotRevision,
		}); err != nil {
			return nil, nil, err
		}
		ackLatencies[index] = time.Since(started)
	}
	return snapshotLatencies, ackLatencies, nil
}

func parallel(count int, concurrency int, fn func(index int) error) error {
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

func summarize(values []time.Duration) Latency {
	if len(values) == 0 {
		return Latency{}
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total time.Duration
	for _, value := range sorted {
		total += value
	}
	return Latency{
		Count: len(sorted),
		Min:   sorted[0],
		Avg:   total / time.Duration(len(sorted)),
		P50:   percentile(sorted, 50),
		P95:   percentile(sorted, 95),
		Max:   sorted[len(sorted)-1],
	}
}

func percentile(sorted []time.Duration, percentile int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted)*percentile+99)/100 - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func nonZero(values []time.Duration) []time.Duration {
	result := make([]time.Duration, 0, len(values))
	for _, value := range values {
		if value > 0 {
			result = append(result, value)
		}
	}
	return result
}
