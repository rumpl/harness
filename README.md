# harness

A Go library for calling different AI coding agent CLIs (Docker Agent, Claude Code, Pi, Codex, opencode) through a unified interface. Switch between agents by changing a single line of code.

## Install

```
go get github.com/rumpl/harness
```

## Quick start

```go
package main

import (
	"context"
	"fmt"

	"github.com/rumpl/harness"
	"github.com/rumpl/harness/dockeragent"
)

func main() {
	// Create a provider — swap this line to switch agents.
	p := dockeragent.New("coder")

	var sessionID string
	// Run the agent and handle streaming events.
	harness.Run(context.Background(), p, "Explain goroutines", func(ev harness.Event) {
		switch ev.Type {
		case harness.EventSessionID:
			sessionID = ev.SessionID
		case harness.EventText:
			fmt.Print(ev.Text)
		case harness.EventToolCallStart:
			fmt.Printf("[tool: %s]\n", ev.ToolName)
		case harness.EventToolCallDelta:
			fmt.Print(ev.ToolArgs)
		case harness.EventToolCall:
			fmt.Printf("[tool: %s] %s\n", ev.ToolName, ev.ToolArgs)
		case harness.EventToolResult:
			fmt.Printf("[tool result: %s] %s\n", ev.ToolName, ev.ToolOutput)
		case harness.EventResult:
			fmt.Printf("\nResult: %s\n", ev.Result)
		}
	})

	// Continue the same conversation. This works with every built-in provider.
	harness.Resume(context.Background(), p, sessionID, "Now show an example", func(ev harness.Event) {
		if ev.Type == harness.EventText {
			fmt.Print(ev.Text)
		}
	})
}
```

### Resuming sessions

Every built-in provider persists fresh sessions and emits an `EventSessionID` event. Save its `SessionID`, then pass it to the provider-independent `Resume` function:

```go
var sessionID string
err := harness.Run(ctx, p, "Start the task", func(ev harness.Event) {
	if ev.Type == harness.EventSessionID {
		sessionID = ev.SessionID
	}
})
if err != nil {
	return err
}

return harness.Resume(ctx, p, sessionID, "Continue the task", handleEvent)
```

Session data remains owned by each agent CLI, so the resumed process must use the same working directory and have access to that CLI's normal session store. Third-party providers can opt in by implementing `harness.ResumableProvider`; providers with a custom streaming transport can additionally implement a matching `Resume` method.

A complete runnable example starts a session, captures its ID, and immediately resumes it:

```bash
go run ./examples/resume -provider claude-code -model claude-sonnet-4-6
```

You can also select another provider:

```bash
go run ./examples/resume -provider codex -model gpt-5.4-mini
go run ./examples/resume -provider pi -model claude-sonnet-4-6
go run ./examples/resume -provider docker-agent -model coder
go run ./examples/resume -provider opencode -model anthropic/claude-sonnet-4-6
```

### Switching providers

```go
// Docker Agent
p := dockeragent.New("coder")

// Claude Code
p := claudecode.New("claude-sonnet-4-6", claudecode.WithEffort(claudecode.EffortHigh))

// Pi
p := pi.New("claude-sonnet-4-6")

// Codex
p := codex.New("gpt-5.4-mini")

// opencode
p := opencode.New("anthropic/claude-sonnet-4-6")
```

The rest of your code stays exactly the same — all providers implement `harness.Provider`.

For providers whose CLIs have their own default model, pass an empty model string to omit the model flag entirely (for example, `codex.New("")` emits `codex exec ...` without `-m`).

## The `Provider` interface

```go
type Provider interface {
	Name() string
	PrintCommand(prompt string) string
	InteractiveArgs(prompt string) []string
	ParseStreamLine(line string) []Event
}

type ResumableProvider interface {
	Provider
	ResumeCommand(sessionID, prompt string) string
}
```

| Method             | Purpose                                                           |
| ------------------ | ----------------------------------------------------------------- |
| `Name()`           | Human-readable identifier (`"docker-agent"`, `"claude-code"`, etc.) |
| `PrintCommand()`   | Shell command for non-interactive mode (`sh -c` safe)             |
| `InteractiveArgs()`| Arg list for interactive mode (first element = binary)            |
| `ParseStreamLine()`| Parse one NDJSON line into `[]Event`                              |
| `ResumeCommand()`  | Build a command that continues a session (optional interface)     |

## Event types

| Type           | Fields set              |
| -------------- | ----------------------- |
| `EventText`    | `Text`                  |
| `EventResult`  | `Result`, `Usage` (opt) |
| `EventToolCallStart` | `ToolID` (opt), `ToolName` |
| `EventToolCallDelta` | `ToolID` (opt), `ToolName` (opt), `ToolArgs` raw delta |
| `EventToolCall`| `ToolID` (opt), `ToolName`, `ToolArgs` |
| `EventToolResult` | `ToolID` (opt), `ToolName` (opt), `ToolOutput`, `ToolError` |
| `EventReasoning` | `Reasoning` |
| `EventSessionID` | `SessionID` |

## Example CLI

```bash
go run ./cmd/harness-example --provider docker-agent --model coder "Hello world"
go run ./cmd/harness-example --provider claude-code --model claude-sonnet-4-6 "Hello world"
go run ./cmd/harness-example --provider pi --model claude-sonnet-4-6 "Hello world"
go run ./cmd/harness-example --provider codex --model gpt-5.4-mini "Hello world"
go run ./cmd/harness-example --provider opencode --model anthropic/claude-sonnet-4-6 "Hello world"

# Resume a session reported by an earlier run:
go run ./cmd/harness-example --provider codex --model gpt-5.4-mini --resume <session-id> "Continue"

# Just print the command without executing:
go run ./cmd/harness-example --print-cmd --provider docker-agent --model coder "test"
```

## License

MIT
