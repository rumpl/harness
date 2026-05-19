// Package opencode provides a [harness.Provider] implementation for the
// opencode CLI agent (https://opencode.ai, https://github.com/sst/opencode).
package opencode

import (
	"fmt"

	"github.com/rumpl/harness"
)

// Option configures an opencode provider.
type Option func(*provider)

// WithAgent sets the --agent flag (e.g. "build", "plan", "general").
func WithAgent(agent string) Option {
	return func(p *provider) { p.agent = agent }
}

// WithThinking enables the --thinking flag so reasoning blocks are streamed.
func WithThinking() Option {
	return func(p *provider) { p.thinking = true }
}

type provider struct {
	model    string
	agent    string
	thinking bool

	// parser carries cross-line state (the last completed assistant text)
	// so a final EventResult can be emitted when the session goes idle.
	parser *parser
}

// New creates an opencode [harness.Provider] for the given model. The model
// must be in opencode's "provider/model" form, e.g.
// "anthropic/claude-sonnet-4-6".
func New(model string, opts ...Option) harness.Provider {
	p := &provider{model: model, parser: &parser{}}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *provider) Name() string { return "opencode" }

func (p *provider) PrintCommand(prompt string) string {
	extra := ""
	if p.model != "" {
		extra += fmt.Sprintf(" --model %s", harness.ShellEscape(p.model))
	}
	if p.agent != "" {
		extra += fmt.Sprintf(" --agent %s", harness.ShellEscape(p.agent))
	}
	if p.thinking {
		extra += " --thinking"
	}
	return fmt.Sprintf(
		"opencode run --format json --dangerously-skip-permissions%s %s",
		extra,
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	args := []string{"opencode"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	if p.agent != "" {
		args = append(args, "--agent", p.agent)
	}
	return args
}

func (p *provider) ParseStreamLine(line string) []Event {
	return p.parser.parseLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
