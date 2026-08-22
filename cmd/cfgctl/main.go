package main

import (
	"flag"
	"fmt"
	"os"

	"config-rollout-plane/internal/configcode"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
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
	for _, diagnostic := range report.Errors {
		fmt.Fprintln(os.Stderr, diagnostic.String())
	}
	if !report.OK() {
		return fmt.Errorf("validation failed: %d error(s)", len(report.Errors))
	}

	fmt.Fprintf(os.Stdout, "validated %d manifest(s) in %d file(s)\n", report.Manifests, report.Files)
	return nil
}

func usageError() error {
	printUsage(os.Stderr)
	return fmt.Errorf("usage: cfgctl validate [file-or-directory ...]")
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  cfgctl validate [file-or-directory ...]")
}
