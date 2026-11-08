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
	"strings"
	"syscall"
	"time"
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
		privateCatalog := flags.String("private-catalog", "", "")
		privateRoot := flags.String("private-root", "", "")
		candidate := flags.String("candidate", "", "")
		fixture := flags.String("fixture", "", "")
		output := flags.String("output", "", "")
		if flags.Parse(args[1:]) != nil || *catalog == "" || *candidate == "" || *fixture == "" || *output == "" ||
			(*privateCatalog == "") != (*privateRoot == "") || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		result, err := evaluation.RunCandidate(ctx, evaluation.CandidateRunRequest{
			CatalogPath: *catalog, PrivateCatalogPath: *privateCatalog, PrivateRoot: *privateRoot,
			CandidatePath: *candidate, FixtureID: *fixture, OutputDir: *output,
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
	case "benchmark":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalog := flags.String("catalog", "", "")
		privateCatalog := flags.String("private-catalog", "", "")
		privateRoot := flags.String("private-root", "", "")
		candidate := flags.String("candidate", "", "")
		fixture := flags.String("fixture", "", "")
		output := flags.String("output", "", "")
		cold := flags.Int("cold", 0, "")
		warm := flags.Int("warm", 0, "")
		timeout := flags.Int("timeout-seconds", 900, "")
		maxRSS := flags.Int64("max-rss-mib", 2048, "")
		maxDisk := flags.Int64("max-temp-disk-mib", 1024, "")
		if flags.Parse(args[1:]) != nil || *catalog == "" || *candidate == "" || *fixture == "" || *output == "" ||
			(*privateCatalog == "") != (*privateRoot == "") ||
			*timeout < 1 || *timeout > 900 || *maxRSS < 1 || *maxDisk < 1 || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		result, err := evaluation.RunBenchmark(ctx, evaluation.BenchmarkRequest{
			CatalogPath: *catalog, PrivateCatalogPath: *privateCatalog, PrivateRoot: *privateRoot,
			CandidatePath: *candidate, FixtureID: *fixture, RunDir: *output,
			Cold: *cold, Warm: *warm, Timeout: time.Duration(*timeout) * time.Second,
			MaxRSSBytes: *maxRSS << 20, MaxTempDiskBytes: *maxDisk << 20,
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
	case "compare":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var receipts stringList
		flags.Var(&receipts, "receipt", "")
		output := flags.String("output", "", "")
		if flags.Parse(args[1:]) != nil || len(receipts) == 0 || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		comparison, err := evaluation.CompareBenchmarkReceipts(receipts)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		if *output != "" {
			if err := evaluation.WriteComparison(*output, comparison); err != nil {
				fmt.Fprintln(stderr, "scanner-eval: output unavailable")
				return 1
			}
		}
		if err := json.NewEncoder(stdout).Encode(comparison); err != nil {
			fmt.Fprintln(stderr, "scanner-eval: output unavailable")
			return 1
		}
		return 0
	case "analyze-corrections":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		catalogPath := flags.String("catalog", "", "")
		oraclePath := flags.String("oracles", "", "")
		var artifacts stringList
		flags.Var(&artifacts, "artifacts", "")
		if flags.Parse(args[1:]) != nil || *catalogPath == "" || *oraclePath == "" || len(artifacts) == 0 || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-eval: invalid input")
			return 2
		}
		catalog, err := evaluation.LoadCatalog(*catalogPath)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		oracles, err := evaluation.LoadCorrectionOracles(*oraclePath)
		if err != nil || evaluation.ValidateCorrectionOracleCoverage(catalog, oracles) != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		comparison, err := evaluation.AnalyzeCorrectionCatalogs(artifacts, oracles)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-eval: evaluation unavailable")
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(comparison); err != nil {
			fmt.Fprintln(stderr, "scanner-eval: output unavailable")
			return 1
		}
		if comparison.Status != "complete" {
			return 1
		}
		return 0
	default:
		fmt.Fprintln(stderr, "scanner-eval: invalid input")
		return 2
	}
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}
