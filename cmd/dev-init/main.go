// Command dev-init binds a worktree to a local development environment and
// initializes its isolated resources and development data. The environment
// identity is a worktree-derived name, not a slot number.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/devinit"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stderr, slog.LevelInfo))
	slog.SetDefault(logger)
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		logger.Error("dev-init stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return runInit(args, stdout)
	}
	switch args[0] {
	case "slot-info":
		if len(args) > 2 {
			usage(stderr)
			return fmt.Errorf("slot-info takes at most one argument")
		}
		jsonOutput := len(args) == 2 && args[1] == "-json"
		if len(args) == 2 && !jsonOutput {
			usage(stderr)
			return fmt.Errorf("unknown argument: %s", args[1])
		}
		repoRoot, err := os.Getwd()
		if err != nil {
			return err
		}
		report, found, err := devinit.CollectInfo(repoRoot)
		if err != nil {
			return err
		}
		if jsonOutput {
			encoded, err := json.Marshal(report)
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, string(encoded))
			return nil
		}
		return devinit.WriteInfo(report, found, stdout)
	case "set-runtime":
		return runSetRuntime(args[1:])
	case "teardown":
		return runTeardown(args[1:], stdout)
	default:
		return runInit(args, stdout)
	}
}

func runInit(args []string, stdout io.Writer) error {
	initFile, update, err := parseInitArgs(args)
	if err != nil {
		return err
	}
	initFile, err = filepath.Abs(initFile)
	if err != nil {
		return err
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	result, err := devinit.Run(context.Background(), repoRoot, initFile, devinit.Options{Update: update})
	if err != nil {
		return err
	}
	printResult(result, stdout)
	return nil
}

func parseInitArgs(args []string) (string, bool, error) {
	update := false
	positionals := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--update" {
			update = true
			continue
		}
		positionals = append(positionals, arg)
	}
	if len(positionals) == 0 {
		return filepath.Join(".run", "dev-init.yaml"), update, nil
	}
	if len(positionals) != 1 {
		return "", false, fmt.Errorf("usage: dev-init [init-file] [--update]")
	}
	return positionals[0], update, nil
}

func runSetRuntime(args []string) error {
	flags := flag.NewFlagSet("set-runtime", flag.ContinueOnError)
	apiPort := flags.Int("api-port", 0, "backend listener port (0 keeps the previous value)")
	webPort := flags.Int("web-port", 0, "frontend listener port (0 keeps the previous value)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *apiPort == 0 && *webPort == 0 {
		return fmt.Errorf("set-runtime requires --api-port and/or --web-port")
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	return devinit.SetRuntime(context.Background(), repoRoot, *apiPort, *webPort)
}

func runTeardown(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("teardown", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "confirm the destructive removal of databases, buckets and markers")
	orphans := flags.Bool("orphans", false, "tear down every registered environment whose worktree is missing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	removed, err := devinit.Teardown(context.Background(), repoRoot, devinit.TeardownOptions{Yes: *yes, Orphans: *orphans})
	for _, name := range removed {
		fmt.Fprintf(stdout, "torn down: %s\n", name)
	}
	return err
}

func printResult(result devinit.Result, stdout io.Writer) {
	layout := result.Layout
	fmt.Fprintf(stdout, "environment initialized: %s (%s)\n", layout.Name, layout.EnvID)
	fmt.Fprintf(stdout, "api: http://%s.localhost\n", layout.APIRoute)
	fmt.Fprintf(stdout, "web: %s\n", layout.WebPublicURL)
	if result.State.Runtime != nil {
		runtime := result.State.Runtime
		fmt.Fprintf(stdout, "stack: pg=%d redis=%d nats=%d minio=%d e2b=%d api=%d web=%d\n",
			runtime.PGPort, runtime.RedisPort, runtime.NATSPort, runtime.MinIOPort, runtime.E2BPort, runtime.APIPort, runtime.WebPort)
	}
	fmt.Fprintf(stdout, "state: %s\n", layout.StateFile)
	fmt.Fprintf(stdout, "credentials: %s\n", layout.CredentialsFile)
	fmt.Fprintf(stdout, "generated_config: %s\n", layout.GeneratedConfig)
}

func usage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage:\n")
	fmt.Fprintf(stderr, "  dev-init [init-file] [--update] (default: .run/dev-init.yaml)\n")
	fmt.Fprintf(stderr, "  dev-init slot-info [-json]\n")
	fmt.Fprintf(stderr, "  dev-init set-runtime --api-port N [--web-port M]\n")
	fmt.Fprintf(stderr, "  dev-init teardown [--yes] [--orphans]\n")
}
