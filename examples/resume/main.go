// Command resume demonstrates starting and then resuming an agent session.
//
// Run it from the repository root, for example:
//
//	go run ./examples/resume -provider claude-code -model claude-sonnet-4-6
//	go run ./examples/resume -provider codex -model gpt-5.4-mini
//	go run ./examples/resume -provider pi -model claude-sonnet-4-6
//	go run ./examples/resume -provider docker-agent -model coder
//	go run ./examples/resume -provider opencode -model anthropic/claude-sonnet-4-6
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/rumpl/harness"
	"github.com/rumpl/harness/claudecode"
	"github.com/rumpl/harness/codex"
	"github.com/rumpl/harness/dockeragent"
	"github.com/rumpl/harness/opencode"
	"github.com/rumpl/harness/pi"
)

func main() {
	providerName := flag.String("provider", "claude-code", "provider: claude-code, codex, pi, docker-agent, or opencode")
	model := flag.String("model", "", "model name; empty uses the provider CLI default")
	firstPrompt := flag.String("first", "Remember that the secret word is marzipan. Reply only with: remembered", "initial prompt")
	followUpPrompt := flag.String("follow-up", "What was the secret word? Reply with only the word.", "prompt sent after resuming")
	flag.Parse()

	provider, err := newProvider(*providerName, *model)
	if err != nil {
		fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var sessionID string
	fmt.Printf("Starting a new %s session...\n\n", provider.Name())
	if err := harness.Run(ctx, provider, *firstPrompt, printEvents(&sessionID)); err != nil {
		fatal(fmt.Errorf("initial run: %w", err))
	}
	if sessionID == "" {
		fatal(errors.New("initial run completed without emitting a session ID"))
	}

	fmt.Printf("\n\nCaptured session ID: %s\n", sessionID)
	fmt.Printf("Resuming that session...\n\n")

	if err := harness.Resume(ctx, provider, sessionID, *followUpPrompt, printEvents(nil)); err != nil {
		fatal(fmt.Errorf("resume: %w", err))
	}
	fmt.Println()
}

func printEvents(sessionID *string) func(harness.Event) {
	return func(event harness.Event) {
		switch event.Type {
		case harness.EventSessionID:
			if sessionID != nil {
				*sessionID = event.SessionID
			}
		case harness.EventText:
			fmt.Print(event.Text)
		case harness.EventToolCallStart:
			fmt.Fprintf(os.Stderr, "\n[tool: %s]", event.ToolName)
		case harness.EventToolCall:
			fmt.Fprintf(os.Stderr, "\n[tool: %s] %s\n", event.ToolName, event.ToolArgs)
		case harness.EventToolResult:
			fmt.Fprintf(os.Stderr, "[tool result: %s] %s\n", event.ToolName, event.ToolOutput)
		}
	}
}

func newProvider(name, model string) (harness.Provider, error) {
	switch name {
	case "claude-code":
		return claudecode.New(model), nil
	case "codex":
		return codex.New(model), nil
	case "pi":
		return pi.New(model), nil
	case "docker-agent":
		if model == "" {
			model = "coder"
		}
		return dockeragent.New(model), nil
	case "opencode":
		if model == "" {
			return nil, errors.New("opencode requires -model in provider/model form")
		}
		return opencode.New(model), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
