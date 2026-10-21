package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Minute

type Runner struct {
	Dir string
	Out io.Writer
	Err io.Writer
}

type Result struct {
	ExitCode int
	Output   string
	Err      error
}

// Run executes a command and streams its output to the runner writers.
func (r *Runner) Run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	command.Dir = r.Dir
	command.Stdout = r.Out
	command.Stderr = r.Err
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Capture executes a command and returns its combined output without writing
// it. Callers decide how to report failures.
func (r *Runner) Capture(name string, args ...string) Result {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	command.Dir = r.Dir
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output

	err := command.Run()
	result := Result{Output: output.String()}
	if ctx.Err() == context.DeadlineExceeded {
		result.Err = fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
		return result
	}
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.ExitCode = exitError.ExitCode()
			result.Err = err
			return result
		}
		result.Err = err
		return result
	}
	return result
}

// Output runs a command and returns trimmed output on success.
func (r *Runner) Output(name string, args ...string) (string, error) {
	result := r.Capture(name, args...)
	if result.Err != nil || result.ExitCode != 0 {
		return result.Output, fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), strings.TrimSpace(result.Output))
	}
	return strings.TrimSpace(result.Output), nil
}
