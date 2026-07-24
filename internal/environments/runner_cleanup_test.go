package environments

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunSandboxStopPhasesUsesFreshCleanupContextAndJoinsErrors(t *testing.T) {
	stoppingErr := errors.New("persist stopping")
	finalStateErr := errors.New("persist final state")
	stopWorkErr := errors.New("stop work")

	err := runSandboxStopPhases(
		5*time.Millisecond,
		time.Second,
		func(ctx context.Context) (error, error) {
			<-ctx.Done()
			return ctx.Err(), errors.Join(stoppingErr, ctx.Err())
		},
		func(ctx context.Context, killErr error) error {
			if !errors.Is(killErr, context.DeadlineExceeded) {
				t.Fatalf("kill error = %v, want deadline exceeded", killErr)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("cleanup context started expired: %v", err)
			}
			return errors.Join(finalStateErr, stopWorkErr)
		},
	)

	for _, want := range []error{stoppingErr, context.DeadlineExceeded, finalStateErr, stopWorkErr} {
		if !errors.Is(err, want) {
			t.Errorf("joined error %v does not contain %v", err, want)
		}
	}
}
