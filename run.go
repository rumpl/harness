package harness

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// streamingProvider is an optional interface for providers that need a custom
// transport to stream events. Providers that do not implement it fall back to
// PrintCommand and ParseStreamLine below.
type streamingProvider interface {
	Run(ctx context.Context, prompt string, fn func(Event)) error
}

// resumingStreamingProvider is the corresponding optional transport for
// continuing an existing session.
type resumingStreamingProvider interface {
	Resume(ctx context.Context, sessionID, prompt string, fn func(Event)) error
}

// Run executes the provider in print (non-interactive) mode and streams
// parsed events to the callback. It blocks until the command finishes or the
// context is cancelled. The callback is invoked synchronously for each event
// as it arrives. A persistent provider emits [EventSessionID], whose SessionID
// can later be passed to [Resume].
func Run(ctx context.Context, p Provider, prompt string, fn func(Event)) error {
	if sp, ok := p.(streamingProvider); ok {
		return sp.Run(ctx, prompt, fn)
	}
	return runCommand(ctx, p, p.PrintCommand(prompt), fn)
}

// Resume continues sessionID with prompt and streams events to fn. It uses the
// same callback contract as [Run]. Resume returns an error when sessionID is
// empty or when p provides neither a resumable command nor a custom resume
// transport.
func Resume(ctx context.Context, p Provider, sessionID, prompt string, fn func(Event)) error {
	if sessionID == "" {
		return errors.New("resume: session ID is empty")
	}
	if sp, ok := p.(resumingStreamingProvider); ok {
		return sp.Resume(ctx, sessionID, prompt, fn)
	}
	rp, ok := p.(ResumableProvider)
	if !ok {
		return fmt.Errorf("resume: provider %q does not support sessions", p.Name())
	}
	return runCommand(ctx, p, rp.ResumeCommand(sessionID, prompt), fn)
}

func runCommand(ctx context.Context, p Provider, command string, fn func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		for _, ev := range p.ParseStreamLine(line) {
			fn(ev)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	return scanner.Err()
}
