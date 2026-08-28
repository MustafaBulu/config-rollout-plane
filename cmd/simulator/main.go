package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/simulator"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			os.Exit(2)
		}
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
		return writeText(os.Stdout, result)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func writeText(out io.Writer, result simulator.Result) error {
	if err := writeLine(out, "agents: %d registered\n", result.RegisteredAgents); err != nil {
		return err
	}
	if err := writeLine(out, "rollout: %s %s\n", result.RolloutID, result.FinalState); err != nil {
		return err
	}
	if err := writeLine(out, "duration: %s\n", result.Duration.Round(time.Millisecond)); err != nil {
		return err
	}
	if err := writeLine(out, "throughput: agents=%.0f/s snapshots=%.0f/s acks=%.0f/s\n", result.AgentsPerSecond, result.SnapshotsPerSecond, result.AcksPerSecond); err != nil {
		return err
	}
	if err := writeLine(out, "snapshots: %d %s\n", result.SnapshotReads, formatLatency(result.SnapshotLatency)); err != nil {
		return err
	}
	if err := writeLine(out, "acks: %d %s\n", result.Acknowledgements, formatLatency(result.AckLatency)); err != nil {
		return err
	}
	for _, stage := range result.StageResults {
		if err := writeLine(out, "stage %s: targets=%d acked=%d coverage=%.2f%% next=%s/%s duration=%s\n",
			stage.StageID,
			stage.Targets,
			stage.Acked,
			stage.Coverage,
			stage.NextState,
			stage.NextStage,
			stage.Duration.Round(time.Millisecond),
		); err != nil {
			return err
		}
	}
	return nil
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

func writeLine(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}
