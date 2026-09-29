package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

func main() { os.Exit(mainCode()) }

func mainCode() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	options, err := parseCLI(os.Args[1:], os.Stdout)
	if errors.Is(err, errHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	root, err := capture(ctx, "", "git", "rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	worker, err := workerImage(root, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if options.Command == "doctor" {
		if isCloudScenario(options.Scenario) {
			if _, err := loadCloudConfig(options); err != nil {
				fmt.Fprintln(os.Stderr, "BLOCKED:", err)
				return 2
			}
			fmt.Fprintln(os.Stdout, "Cloud configuration valid; credentials, remote behavior and cleanup require a scenario run.")
			return 0
		}
		result, err := doctor(ctx, root, worker, options.Scenario)
		if err != nil {
			fmt.Fprintln(os.Stderr, "BLOCKED:", err)
			return 2
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return 1
		}
		return 0
	}
	selected := selectScenario(options.Scenario)
	if options.Timeout > 0 {
		selected.Timeout = options.Timeout
	}
	lock, err := os.OpenFile(filepath.Join(os.TempDir(), "oma-verify-be-18080.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "BLOCKED: another verification owns port 18080:", err)
		return 2
	}
	return run(ctx, root, worker, options, selected)
}

func run(ctx context.Context, root, worker string, options cliOptions, selected scenario) int {
	runID := "verify-be-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	directory := filepath.Join(root, "tmp", "verify-be", runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	r := report{RunID: runID, Scenario: options.Scenario, Status: "incomplete", CleanupErrors: []string{}, Diagnostics: options.Diagnostics, Timeout: selected.Timeout.String()}
	fmt.Println("Evidence:", directory)
	err := perform(ctx, root, directory, worker, options, selected, &r)
	if err != nil {
		r.Error = err.Error()
		if r.FailureKind == "" {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				r.FailureKind = "orchestration_timeout"
			case errors.Is(err, context.Canceled):
				r.FailureKind = "cancelled"
			default:
				r.FailureKind = "execution"
			}
		}
		if r.Status != "blocked" {
			r.Status = "fail"
		}
	}
	if err := saveReport(directory, r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s: %s\n", strings.ToUpper(r.Status), filepath.Join(directory, "report.md"))
	switch r.Status {
	case "pass", "passed_with_skips":
		return 0
	case "blocked":
		return 2
	default:
		return 1
	}
}

func perform(ctx context.Context, root, directory, worker string, options cliOptions, selected scenario, r *report) error {
	var err error
	r.Source, err = sourceIdentity(ctx, root)
	if err != nil {
		return err
	}
	if strings.HasSuffix(r.Scenario, ".performance") {
		r.WorkloadSHA256, err = performanceWorkloadHash(root, r.Scenario)
		if err != nil {
			return err
		}
	}
	if err := saveReport(directory, *r); err != nil {
		return err
	}
	if isCloudScenario(r.Scenario) {
		return performCloud(ctx, root, directory, options, selected, r)
	}
	r.Doctor, err = doctor(ctx, root, worker, options.Scenario)
	if err != nil {
		r.Status = "blocked"
		r.FailureKind = "prerequisite"
		r.CleanupComplete = !errors.Is(err, errDoctorCleanup)
		if !r.CleanupComplete {
			r.Status = "fail"
			r.CleanupErrors = append(r.CleanupErrors, err.Error())
		}
		return err
	}
	env := newEnvironment(root, directory, r.RunID)
	env.backendRef, env.diagnostics = options.BackendRef, options.Diagnostics
	env.files = strings.HasPrefix(options.Scenario, "files.")
	env.suite = options.Scenario == "test"
	defer func() {
		r.CleanupErrors = env.close()
		r.CleanupComplete = len(r.CleanupErrors) == 0
		if !r.CleanupComplete {
			r.Status = "fail"
		}
	}()
	if env.suite {
		fmt.Println("Starting isolated dependencies for the Go suite…")
	} else {
		fmt.Println("Starting isolated dependencies and building the backend…")
	}
	if err := env.start(ctx, r.Doctor.Images); err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	if !env.suite {
		r.BinarySHA256, err = fileSHA256(filepath.Join(directory, "server"))
		if err != nil {
			return err
		}
	}
	r.BackendCommit = env.backendCommit
	fmt.Println("Running", r.Scenario, "in the isolated environment…")
	finishDiagnostics := env.captureDiagnostics(ctx)
	r.Result, err = runTest(ctx, env, r.Doctor.Images["worker"], selected)
	if diagnosticErr := finishDiagnostics(); diagnosticErr != nil && err == nil {
		err = diagnosticErr
	}
	if err != nil {
		return err
	}
	r.Status = r.Result.Status
	if r.Status == "fail" {
		r.FailureKind = r.Result.FailureKind
	}
	if strings.HasSuffix(r.Scenario, ".performance") && r.Status == "pass" {
		if err := evaluatePerformance(r, options.Baseline); err != nil {
			if r.Status == "blocked" {
				r.FailureKind = "baseline_incompatible"
			} else {
				r.FailureKind = "performance_regression"
			}
			return err
		}
	}
	after, err := sourceIdentity(ctx, root)
	if err != nil {
		return err
	}
	if after != r.Source {
		return errors.New("source changed during verification; rerun against a stable checkout")
	}
	return nil
}

func runTest(ctx context.Context, env *environment, worker string, selected scenario) (testResult, error) {
	path := filepath.Join(env.directory, "tests.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return testResult{}, err
	}
	defer file.Close()
	stderr, err := os.OpenFile(filepath.Join(env.directory, "tests.stderr.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return testResult{}, err
	}
	defer stderr.Close()
	values := append(env.env(), "OMA_WORKER_CONTROL_IMAGE="+worker, "VERIFY_BE_TIMEOUT="+selected.Timeout.String())
	args := []string{"go", "test", selected.Package, "-json", "-count=1", "-timeout=" + (selected.Timeout + 30*time.Second).String()}
	if env.suite {
		values, err = suiteEnvironment(env)
		if err != nil {
			return testResult{}, err
		}
	} else {
		args = append(args, "-run", "^"+selected.Test+"$")
	}
	err = execute(ctx, env.root, values, file, stderr, selected.Timeout+2*time.Minute, args...)
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return testResult{}, err
		}
		code = exit.ExitCode()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return testResult{}, err
	}
	if env.suite {
		return evaluateSuite(file, code)
	}
	return evaluate(file, code, selected)
}

func workerImage(root string, options cliOptions) (string, error) {
	if !needsWorker(options.Scenario) {
		return "", nil
	}
	return resolveWorkerImage(root, options.WorkerImage, os.Getenv("OMA_WORKER_CONTROL_IMAGE"))
}
