// Command harness-example demonstrates how to use the harness library to
// run an AI coding agent and process its streaming output. It supports all
// five providers (claude-code, pi, codex, docker-agent, opencode) and lets
// you switch between them with a single flag.
//
// Usage:
//
//	harness-example --provider claude-code --model claude-opus-4-6 "Explain what a goroutine is"
//	harness-example --provider pi --model claude-sonnet-4-6 "Write hello world in Go"
//	harness-example --provider codex --model gpt-5.4-mini "List files in /tmp"
//	harness-example --provider docker-agent --model coder "Hello"
//	harness-example --provider opencode --model anthropic/claude-sonnet-4-6 "Hello"
//	harness-example --print-cmd --provider codex --model gpt-5.4-mini "test"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/rumpl/harness"
	"github.com/rumpl/harness/claudecode"
	"github.com/rumpl/harness/codex"
	"github.com/rumpl/harness/dockeragent"
	"github.com/rumpl/harness/opencode"
	"github.com/rumpl/harness/pi"
)

func main() {
	providerName := flag.String("provider", "claude-code", "provider to use: claude-code, pi, codex, docker-agent, opencode")
	model := flag.String("model", "claude-sonnet-4-6", "model name to pass to the provider")
	effort := flag.String("effort", "", "effort level for claude-code (low, medium, high, max)")
	printCmd := flag.Bool("print-cmd", false, "print the shell command instead of executing it")
	printArgs := flag.Bool("print-args", false, "print the interactive args instead of executing")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: harness-example [flags] <prompt>")
		os.Exit(1)
	}
	prompt := strings.Join(flag.Args(), " ")

	p := newProvider(*providerName, *model, *effort)

	if *printCmd {
		fmt.Println(p.PrintCommand(prompt))
		return
	}
	if *printArgs {
		fmt.Println(strings.Join(p.InteractiveArgs(prompt), " "))
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	fmt.Fprintf(os.Stderr, "provider: %s\n", p.Name())
	fmt.Fprintf(os.Stderr, "prompt:   %s\n\n", prompt)

	err := harness.Run(ctx, p, prompt, func(ev harness.Event) {
		switch ev.Type {
		case harness.EventText:
			fmt.Print(ev.Text)
		case harness.EventToolCallStart:
			fmt.Fprintf(os.Stderr, "\n[tool: %s]\n", ev.ToolName)
		case harness.EventToolCallDelta:
			fmt.Fprintf(os.Stderr, "%s", ev.ToolArgs)
		case harness.EventToolCall:
			fmt.Fprintf(os.Stderr, "\n[tool: %s] %s\n", ev.ToolName, ev.ToolArgs)
		case harness.EventToolResult:
			fmt.Fprintf(os.Stderr, "\n[tool result: %s] %s\n", ev.ToolName, ev.ToolOutput)
		case harness.EventResult:
			fmt.Fprintf(os.Stderr, "\n--- result ---\n")
			fmt.Println(ev.Result)
			if ev.Usage != nil {
				fmt.Fprintf(os.Stderr,
					"tokens: %d in / %d out | cost: $%.4f | turns: %d | %dms\n",
					ev.Usage.InputTokens,
					ev.Usage.OutputTokens,
					ev.Usage.TotalCostUSD,
					ev.Usage.NumTurns,
					ev.Usage.DurationMS,
				)
			}
		}
	})
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newProvider(name, model, effort string) harness.Provider {
	switch name {
	case "claude-code":
		var opts []claudecode.Option
		if effort != "" {
			opts = append(opts, claudecode.WithEffort(claudecode.Effort(effort)))
		}
		return claudecode.New(model, opts...)
	case "pi":
		return pi.New(model)
	case "codex":
		return codex.New(model)
	case "docker-agent":
		return dockeragent.New(model)
	case "opencode":
		return opencode.New(model)
	default:
		fmt.Fprintf(os.Stderr, "unknown provider: %s (choices: claude-code, pi, codex, docker-agent, opencode)\n", name)
		os.Exit(1)
		return nil
	}
}
