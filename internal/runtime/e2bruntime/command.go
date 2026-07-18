package e2bruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func (p *E2BProvider) RunCommand(ctx context.Context, sandboxID string, command string, timeout time.Duration) error {
	if strings.TrimSpace(sandboxID) == "" {
		return errors.New("sandbox id is required")
	}
	if strings.TrimSpace(command) == "" {
		return errors.New("sandbox command is required")
	}
	sandbox, err := p.connect(ctx, sandboxID)
	if err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = p.cfg.E2BRequestTimeout
	}
	timeoutMs := int(timeout / time.Millisecond)
	if timeoutMs <= 0 {
		timeoutMs = int((60 * time.Second) / time.Millisecond)
	}
	execution, err := sandbox.Commands.Run(ctx, command, &e2b.CommandStartOpts{TimeoutMs: &timeoutMs})
	if err != nil {
		var exitErr *e2b.CommandExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("sandbox command exited with code %d: %s stdout=%q stderr=%q", exitErr.ExitCode, strings.TrimSpace(exitErr.Message), truncateCommandOutput(exitErr.Stdout), truncateCommandOutput(exitErr.Stderr))
		}
		return err
	}
	result, ok := execution.(*e2b.CommandResult)
	if !ok {
		return fmt.Errorf("sandbox command execution type = %T, want *e2b.CommandResult", execution)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("sandbox command exited with code %d: %s stdout=%q stderr=%q", result.ExitCode, strings.TrimSpace(result.Error), truncateCommandOutput(result.Stdout), truncateCommandOutput(result.Stderr))
	}
	return nil
}
