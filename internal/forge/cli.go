package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// errStopStream tells runForgeCLIStream that its consumer wants no more
// output. The CLI is stopped and the call reports success: a consumer that
// reached its row budget is done rather than broken.
var errStopStream = errors.New("forge: stream consumer is done")

// runForgeCLI executes a forge CLI request while keeping stdout separate from
// diagnostics. Forge-specific wrappers own argument and payload construction.
//
// The context bounds the subprocess: cancelling it kills the CLI rather than
// leaving a request running for results nobody will read.
func runForgeCLI(ctx context.Context, tool string, args []string, input []byte, label string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, forgeCLIError(ctx, label, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// runForgeCLIStream executes a forge CLI request and hands stdout to consume
// while the command is still running, so a caller can act on the first results
// before the CLI has finished fetching the rest.
//
// consume is expected to read until EOF. Returning early stops the CLI.
// Returning errStopStream stops it without reporting a failure.
func runForgeCLIStream(ctx context.Context, tool string, args []string, label string, consume func(io.Reader) error) error {
	parentCtx := ctx
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := cmd.Start(); err != nil {
		return forgeCLIError(ctx, label, stderr.String(), err)
	}

	consumeErr := consume(stdout)
	stopped := errors.Is(consumeErr, errStopStream)
	if consumeErr != nil {
		// Nothing further is wanted from the CLI, so stop it rather than let
		// it keep fetching pages nobody will read.
		cancel()
	}
	// Drain and reap, so the subprocess never outlives this call.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	// A cancelled parent commonly truncates the record the decoder was reading.
	// Report the cause rather than that secondary parse or empty-output error.
	if err := parentCtx.Err(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	// The consumer stopped on purpose, so the killed CLI is not a failure.
	if stopped {
		return nil
	}
	if consumeErr != nil {
		// A CLI that printed a diagnostic explains the failure better than the
		// decode error that reading its empty output produced.
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return fmt.Errorf("%s: %s", label, message)
		}
		return consumeErr
	}
	if waitErr != nil {
		return forgeCLIError(ctx, label, stderr.String(), waitErr)
	}
	return nil
}

// forgeCLIError explains a failed CLI request. A command the context killed
// reports the cancellation rather than the signal it died of, so a caller that
// cancelled deliberately — a closed picker, say — can tell its own doing from
// a request that really failed.
func forgeCLIError(ctx context.Context, label, stderr string, err error) error {
	if cancelled := ctx.Err(); cancelled != nil {
		return fmt.Errorf("%s: %w", label, cancelled)
	}
	if message := strings.TrimSpace(stderr); message != "" {
		return fmt.Errorf("%s: %s", label, message)
	}
	return fmt.Errorf("%s: %w", label, err)
}
