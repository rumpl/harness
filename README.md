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

	// Run the agent and handle streaming events.
	harness.Run(context.Background(), p, "Explain goroutines", func(ev harness.Event) {
		switch ev.Type {
		case harness.EventText:
			fmt.Print(ev.Text)
		case harness.EventToolCall:
			fmt.Printf("[tool: %s] %s\n", ev.ToolName, ev.ToolArgs)
		case harness.EventResult:
			fmt.Printf("\nResult: %s\n", ev.Result)
		}
	})
}
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

## The `Provider` interface

```go
type Provider interface {
	Name() string
	PrintCommand(prompt string) string
	InteractiveArgs(prompt string) []string
	ParseStreamLine(line string) []Event
}
```

| Method             | Purpose                                                           |
| ------------------ | ----------------------------------------------------------------- |
| `Name()`           | Human-readable identifier (`"docker-agent"`, `"claude-code"`, etc.) |
| `PrintCommand()`   | Shell command for non-interactive mode (`sh -c` safe)             |
| `InteractiveArgs()`| Arg list for interactive mode (first element = binary)            |
| `ParseStreamLine()`| Parse one NDJSON line into `[]Event`                              |

## Event types

| Type           | Fields set              |
| -------------- | ----------------------- |
| `EventText`    | `Text`                  |
| `EventResult`  | `Result`, `Usage` (opt) |
| `EventToolCall`| `ToolName`, `ToolArgs`  |

## Example CLI

```bash
go run ./cmd/harness-example --provider docker-agent --model coder "Hello world"
go run ./cmd/harness-example --provider claude-code --model claude-sonnet-4-6 "Hello world"
go run ./cmd/harness-example --provider pi --model claude-sonnet-4-6 "Hello world"
go run ./cmd/harness-example --provider codex --model gpt-5.4-mini "Hello world"
go run ./cmd/harness-example --provider opencode --model anthropic/claude-sonnet-4-6 "Hello world"

# Just print the command without executing:
go run ./cmd/harness-example --print-cmd --provider docker-agent --model coder "test"
```

## License

MIT
