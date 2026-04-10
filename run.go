package harness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// Run executes the provider in print (non-interactive) mode and streams
// parsed events to the callback. It blocks until the command finishes or the
// context is cancelled. The callback is invoked synchronously for each event
// as it arrives.
func Run(ctx context.Context, p Provider, prompt string, fn func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", p.PrintCommand(prompt))
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
