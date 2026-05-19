// Package claudecode provides a [harness.Provider] implementation for the
// Claude Code CLI agent.
package claudecode

import (
	"fmt"
	"strings"

	"github.com/rumpl/harness"
)

// Effort controls the effort level passed to the Claude Code CLI.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

// Option configures a Claude Code provider.
type Option func(*provider)

// WithEffort sets the --effort flag.
func WithEffort(e Effort) Option {
	return func(p *provider) { p.effort = e }
}

type provider struct {
	model  string
	effort Effort
	parser *parser
}

// New creates a Claude Code [harness.Provider] for the given model.
//
// Claude Code expects bare Claude model IDs (for example
// "claude-sonnet-4-6"), but callers may provide Anthropic model IDs in the
// provider/model form used by other CLIs (for example
// "anthropic/claude-sonnet-4-6"). The anthropic/ prefix is stripped before
// invoking the Claude Code CLI.
func New(model string, opts ...Option) harness.Provider {
	p := &provider{model: normalizeModel(model), parser: newParser()}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *provider) Name() string { return "claude-code" }

func normalizeModel(model string) string {
	const providerPrefix = "anthropic/"
	return strings.TrimPrefix(model, providerPrefix)
}

func (p *provider) PrintCommand(prompt string) string {
	modelFlag := ""
	if p.model != "" {
		modelFlag = fmt.Sprintf(" --model %s", harness.ShellEscape(p.model))
	}
	effortFlag := ""
	if p.effort != "" {
		effortFlag = fmt.Sprintf(" --effort %s", p.effort)
	}
	return fmt.Sprintf(
		"claude --print --verbose --dangerously-skip-permissions --include-partial-messages --output-format stream-json%s%s -p %s",
		modelFlag,
		effortFlag,
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	args := []string{"claude", "--dangerously-skip-permissions"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	if p.effort != "" {
		args = append(args, "--effort", string(p.effort))
	}
	return args
}

func (p *provider) ParseStreamLine(line string) []Event {
	if p.parser == nil {
		p.parser = newParser()
	}
	return p.parser.parseLine(line)
}

// Event is an alias for [harness.Event] so callers importing only this
// package still have access to the type.
type Event = harness.Event
