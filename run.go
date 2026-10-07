package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
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
// can later be passed to [Resume], even if the run fails or is canceled.
// Command-based providers accept lines up to 16 MiB and bound post-exit pipe
// draining to two seconds of idle time, excluding time in callbacks.
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

const (
	maxStreamLineBytes = 16 * 1024 * 1024
	streamDrainTimeout = 2 * time.Second
)

// StreamValidator validates a single command invocation's output. Validation
// runs before parsing; EndStream is called only after a clean EOF. Implementations
// must not include untrusted stream contents (which may contain secrets) in errors.
type StreamValidator interface {
	ValidateStreamLine(line string) error
	EndStream() error
}

// streamValidatingProvider supplies fresh validation state for each Run or Resume.
type streamValidatingProvider interface {
	NewStreamValidator() StreamValidator
}

func runCommand(ctx context.Context, p Provider, command string, fn func(Event)) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	configureProcess(cmd)
	cmd.WaitDelay = streamDrainTimeout
	// Own the pipe: Cmd.Wait must not close stdout before buffered events are read.
	stdout, output, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	defer stdout.Close()
	cmd.Stdout = output
	// Discard diagnostics rather than risk exposing credentials or tool contents.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		return fmt.Errorf("start: %w", err)
	}
	_ = output.Close()
	waited := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(exited)
		waited <- err
	}()
	var validator StreamValidator
	if vp, ok := p.(streamValidatingProvider); ok {
		validator = vp.NewStreamValidator()
	}
	stream := lineStream{handle: func(line string) error {
		if validator != nil {
			if err := validator.ValidateStreamLine(line); err != nil {
				return err
			}
		}
		for _, ev := range p.ParseStreamLine(line) {
			fn(ev)
		}
		return nil
	}}
	readErr := readOutput(runCtx, stdout, exited, &stream)
	if readErr == nil {
		readErr = stream.finish()
	}
	if readErr == nil && validator != nil {
		readErr = validator.EndStream()
	}
	if readErr != nil {
		// The leader may already be gone while descendants still own the pipe.
		_ = cmd.Cancel()
		cancel()
	}
	waitErr := <-waited
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return fmt.Errorf("read stream: %w", readErr)
	}
	if waitErr != nil {
		_ = cmd.Cancel()
		return fmt.Errorf("wait: %w", waitErr)
	}
	return nil
}

func readOutput(ctx context.Context, stdout *os.File, exited <-chan struct{}, stream *lineStream) error {
	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult)
	stop := make(chan struct{})
	done := make(chan struct{})
	defer func() {
		close(stop)
		_ = stdout.Close()
		<-done
	}()
	go func() {
		defer close(done)
		for {
			data := make([]byte, 32*1024)
			n, err := stdout.Read(data)
			select {
			case reads <- readResult{data[:n], err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var deadline <-chan time.Time
	remaining := streamDrainTimeout
	draining := false
	for {
		start := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			exited = nil
			draining = true
			timer.Reset(remaining)
			deadline = timer.C
		case <-deadline:
			return errors.New("stdout remained open after process exit")
		case result := <-reads:
			if draining {
				remaining -= time.Since(start)
				timer.Stop()
			}
			// Synchronous callbacks are back-pressure, not pipe idle time.
			if err := stream.write(result.data); err != nil {
				return err
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return nil
				}
				return result.err
			}
			if draining {
				if remaining <= 0 {
					return errors.New("stdout remained open after process exit")
				}
				timer.Reset(remaining)
			}
		}
	}
}

type lineStream struct {
	pending []byte
	handle  func(string) error
}

func (s *lineStream) write(data []byte) error {
	for len(data) > 0 {
		line, rest, found := bytes.Cut(data, []byte{'\n'})
		if len(line) > maxStreamLineBytes-len(s.pending) {
			return fmt.Errorf("stream line exceeds %d bytes", maxStreamLineBytes)
		}
		s.pending = append(s.pending, line...)
		if !found {
			break
		}
		if err := s.handle(string(bytes.TrimSuffix(s.pending, []byte{'\r'}))); err != nil {
			return err
		}
		s.pending = s.pending[:0]
		data = rest
	}
	return nil
}

func (s *lineStream) finish() error {
	if len(s.pending) == 0 {
		return nil
	}
	return s.handle(string(bytes.TrimSuffix(s.pending, []byte{'\r'})))
}
