package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"config-rollout-plane/internal/configcode"
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
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "apply":
		return runApply(args[1:])
	case "help", "-h", "--help":
		return printUsage(os.Stdout)
	default:
		return usageError()
	}
}

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}

	report := configcode.Validator{}.ValidatePaths(flags.Args())
	if err := printDiagnostics(os.Stderr, report.Errors); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("validation failed: %d error(s)", len(report.Errors))
	}

	return writeLine(os.Stdout, "validated %d manifest(s) in %d file(s)\n", report.Manifests, report.Files)
}

func runApply(args []string) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	controlPlaneURL := flags.String("control-plane-url", os.Getenv("SAFECONFIG_CONTROL_PLANE_URL"), "control-plane base URL")
	token := flags.String("token", os.Getenv("SAFECONFIG_TOKEN"), "bearer token")
	dryRun := flags.Bool("dry-run", false, "print the apply plan without writing")
	includeRollouts := flags.Bool("include-rollouts", false, "start rollout manifests during apply")
	if err := flags.Parse(args); err != nil {
		return err
	}

	report := configcode.Validator{}.ValidatePaths(flags.Args())
	if err := printDiagnostics(os.Stderr, report.Errors); err != nil {
		return err
	}
	if !report.OK() {
		return fmt.Errorf("validation failed: %d error(s)", len(report.Errors))
	}

	applier := configcode.Applier{
		Options: configcode.ApplyOptions{
			BaseURL:         *controlPlaneURL,
			Token:           *token,
			DryRun:          *dryRun,
			IncludeRollouts: *includeRollouts,
		},
	}
	applyReport, err := applier.ApplyPaths(context.Background(), flags.Args())
	if err != nil {
		return err
	}
	for _, step := range applyReport.Steps {
		if err := writeLine(os.Stdout, "%s %s %s: %s\n", step.Status, step.Action, step.Kind, step.Name); err != nil {
			return err
		}
	}
	return writeLine(os.Stdout, "processed %d step(s)\n", len(applyReport.Steps))
}

func usageError() error {
	if err := printUsage(os.Stderr); err != nil {
		return err
	}
	return fmt.Errorf("usage: cfgctl <command> [options]")
}

func printUsage(out io.Writer) error {
	if err := writeLine(out, "Usage:\n"); err != nil {
		return err
	}
	if err := writeLine(out, "  cfgctl validate [file-or-directory ...]\n"); err != nil {
		return err
	}
	return writeLine(out, "  cfgctl apply [--dry-run] [--include-rollouts] [--control-plane-url URL] [--token TOKEN] [file-or-directory ...]\n")
}

func printDiagnostics(out io.Writer, diagnostics []configcode.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		if err := writeLine(out, "%s\n", diagnostic.String()); err != nil {
			return err
		}
	}
	return nil
}

func writeLine(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format, args...)
	return err
}
