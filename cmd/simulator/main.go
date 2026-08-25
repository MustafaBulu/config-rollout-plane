package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/simulator"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("simulator", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	agentCount := flags.Int("agents", 1000, "number of virtual agents")
	concurrency := flags.Int("concurrency", 64, "maximum concurrent virtual agents")
	service := flags.String("service", "payment-service", "agent service name")
	environment := flags.String("environment", string(domain.EnvironmentProduction), "agent environment")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return err
	}

	result, err := simulator.Run(context.Background(), simulator.Options{
		AgentCount:  *agentCount,
		Concurrency: *concurrency,
		Service:     *service,
		Environment: domain.Environment(*environment),
	})
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	case "text":
		printText(result)
		return nil
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func printText(result simulator.Result) {
	fmt.Printf("agents: %d registered\n", result.RegisteredAgents)
	fmt.Printf("rollout: %s %s\n", result.RolloutID, result.FinalState)
	fmt.Printf("duration: %s\n", result.Duration.Round(time.Millisecond))
	fmt.Printf("throughput: agents=%.0f/s snapshots=%.0f/s acks=%.0f/s\n", result.AgentsPerSecond, result.SnapshotsPerSecond, result.AcksPerSecond)
	fmt.Printf("snapshots: %d %s\n", result.SnapshotReads, formatLatency(result.SnapshotLatency))
	fmt.Printf("acks: %d %s\n", result.Acknowledgements, formatLatency(result.AckLatency))
	for _, stage := range result.StageResults {
		fmt.Printf("stage %s: targets=%d acked=%d coverage=%.2f%% next=%s/%s duration=%s\n",
			stage.StageID,
			stage.Targets,
			stage.Acked,
			stage.Coverage,
			stage.NextState,
			stage.NextStage,
			stage.Duration.Round(time.Millisecond),
		)
	}
}

func formatLatency(latency simulator.Latency) string {
	return fmt.Sprintf("latency[min=%s avg=%s p50=%s p95=%s max=%s]",
		latency.Min.Round(time.Microsecond),
		latency.Avg.Round(time.Microsecond),
		latency.P50.Round(time.Microsecond),
		latency.P95.Round(time.Microsecond),
		latency.Max.Round(time.Microsecond),
	)
}
