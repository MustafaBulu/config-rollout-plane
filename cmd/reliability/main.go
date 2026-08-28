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
	"config-rollout-plane/internal/reliability"
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
	flags := flag.NewFlagSet("reliability", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	scenario := flags.String("scenario", "all", "scenario: all, control-plane-restart, data-plane-outage, concurrent-rollout-acknowledgements, rollback-propagation-timing")
	concurrency := flags.Int("concurrency", 32, "maximum concurrent agents")
	service := flags.String("service", "payment-service", "agent service name")
	environment := flags.String("environment", string(domain.EnvironmentProduction), "agent environment")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "safeconfig-reliability-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.RemoveAll(tempDir); removeErr != nil {
			if _, writeErr := fmt.Fprintf(os.Stderr, "remove temp dir: %v\n", removeErr); writeErr != nil {
				return
			}
		}
	}()

	options := reliability.Options{
		Service:     *service,
		Environment: domain.Environment(*environment),
		TempDir:     tempDir,
		Concurrency: *concurrency,
	}
	results, err := runSelected(context.Background(), *scenario, options)
	if err != nil {
		return err
	}

	switch *format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	case "text":
		return writeText(os.Stdout, results)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func runSelected(ctx context.Context, scenario string, options reliability.Options) ([]reliability.ScenarioResult, error) {
	runners := map[string]func(context.Context, reliability.Options) (reliability.ScenarioResult, error){
		"control-plane-restart":               reliability.RunControlPlaneRestartScenario,
		"data-plane-outage":                   reliability.RunDataPlaneOutageScenario,
		"concurrent-rollout-acknowledgements": reliability.RunConcurrentRolloutAcknowledgementScenario,
		"rollback-propagation-timing":         reliability.RunRollbackPropagationTimingScenario,
	}
	if scenario != "all" {
		runner, ok := runners[scenario]
		if !ok {
			return nil, fmt.Errorf("unknown scenario %q", scenario)
		}
		result, err := runner(ctx, options)
		return []reliability.ScenarioResult{result}, err
	}

	order := []string{
		"control-plane-restart",
		"data-plane-outage",
		"concurrent-rollout-acknowledgements",
		"rollback-propagation-timing",
	}
	results := make([]reliability.ScenarioResult, 0, len(order))
	for _, name := range order {
		result, err := runners[name](ctx, options)
		results = append(results, result)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func writeText(out io.Writer, results []reliability.ScenarioResult) error {
	for _, result := range results {
		status := "FAILED"
		if result.Passed {
			status = "PASSED"
		}
		if err := writeLine(out, "scenario %s: %s duration=%s\n", result.Name, status, result.Duration.Round(time.Millisecond)); err != nil {
			return err
		}
		for _, event := range result.Events {
			eventStatus := "FAILED"
			if event.Passed {
				eventStatus = "PASSED"
			}
			if err := writeLine(out, "  step %s: %s duration=%s\n", event.Name, eventStatus, event.Duration.Round(time.Millisecond)); err != nil {
				return err
			}
		}
		for name, value := range result.Metrics {
			if err := writeLine(out, "  metric %s=%d\n", name, value); err != nil {
				return err
			}
		}
		for name, value := range result.Timings {
			if err := writeLine(out, "  timing %s=%s\n", name, value.Round(time.Millisecond)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeLine(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}
