package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"scanner-kiosk/internal/evaluation"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "scanner-eval: invalid input")
		return 2
	}
	switch args[0] {
	case "generate-fixtures":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalog := flags.String("catalog", "", "")
		output := flags.String("output", "", "")
		manifest := flags.String("manifest", "", "")
		if flags.Parse(args[1:]) != nil || *catalog == "" || *output == "" || *manifest == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		if err := evaluation.Generate(*catalog, *output, *manifest); err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		return 0
	case "list-fixtures":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalog := flags.String("catalog", "", "")
		privateCatalog := flags.String("private-catalog", "", "")
		privateRoot := flags.String("private-root", "", "")
		if flags.Parse(args[1:]) != nil || *catalog == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		manifest, err := evaluation.ListWithPrivate(*catalog, *privateCatalog, *privateRoot)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(manifest); err != nil {
			fmt.Fprintln(stderr, "scanner-eval: output unavailable")
			return 1
		}
		return 0
	case "run":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalog := flags.String("catalog", "", "")
		candidate := flags.String("candidate", "", "")
		fixture := flags.String("fixture", "", "")
		output := flags.String("output", "", "")
		if flags.Parse(args[1:]) != nil || *catalog == "" || *candidate == "" || *fixture == "" || *output == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		result, err := evaluation.RunCandidate(ctx, evaluation.CandidateRunRequest{
			CatalogPath: *catalog, CandidatePath: *candidate, FixtureID: *fixture, OutputDir: *output,
		})
		if result.RunID != "" {
			if encodeErr := json.NewEncoder(stdout).Encode(result); encodeErr != nil {
				fmt.Fprintln(stderr, "scanner-eval: output unavailable")
				return 1
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		return 0
	default:
		fmt.Fprintln(stderr, "scanner-eval: invalid input")
		return 2
	}
}
