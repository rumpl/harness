// Package commandcode provides a [harness.Provider] implementation for the
// Command Code CLI agent (binary name: "cmd").
//
// The CLI is driven in non-interactive mode with:
//
//	cmd -p --output-format json --yolo <prompt>
//
// which prints newline-delimited JSON on stdout. See parse.go for the event
// mapping.
package commandcode

import (
	"strings"

	"github.com/rumpl/harness"
)

// Command-code flag literals. Defined once so each flag name has a single
// occurrence in the source and every builder shares it.
const (
	outputFormat = "json"
	modelFlag    = "--model"
	effortFlag   = "--effort"
	resumeFlag   = "-r"
)

// Option configures a command-code provider.
type Option func(*provider)

// WithEffort sets the --effort flag (e.g. "low", "medium", "high").
func WithEffort(effort string) Option {
	return func(p *provider) { p.effort = effort }
}

type provider struct {
	model  string
	effort string

	// parser mirrors the sibling providers. Most command-code NDJSON lines are
	// self-describing, but the parser carries the turn count seen on run_end
	// so it can be attached to the following result line's Usage.NumTurns.
	parser *parser
}

// New creates a command-code [harness.Provider] for the given model. An empty
// model lets the CLI pick its own default.
func New(model string, opts ...Option) harness.Provider {
	p := &provider{model: model, parser: &parser{}}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (p *provider) Name() string { return "command-code" }

func (p *provider) PrintCommand(prompt string) string {
	return p.printCommand("", prompt)
}

func (p *provider) ResumeCommand(sessionID, prompt string) string {
	return p.printCommand(sessionID, prompt)
}

// printCommand renders the shell command string from the same argv that
// [provider.args] produces, so there is exactly one place that decides which
// flags are present. The executable, the fixed output-format value and any
// dash-prefixed flag tokens are emitted verbatim; caller-supplied values
// (model, effort, session id, prompt) are shell-escaped.
func (p *provider) printCommand(sessionID, prompt string) string {
	argv := p.args(sessionID, prompt)
	parts := make([]string, len(argv))
	for i, a := range argv {
		if i == 0 || strings.HasPrefix(a, "-") || a == outputFormat {
			parts[i] = a
			continue
		}
		parts[i] = harness.ShellEscape(a)
	}
	return strings.Join(parts, " ")
}

// args builds the full argv for one turn. It is the single source of truth for
// the command-code flag list: the fixed prefix (executable, -p,
// --output-format with its json value, --yolo), then the model and effort
// flags when set, then the resume flag when set, then the prompt.
func (p *provider) args(sessionID, prompt string) []string {
	argv := []string{"cmd", "-p", "--output-format", outputFormat, "--yolo"}
	if p.model != "" {
		argv = append(argv, modelFlag, p.model)
	}
	if p.effort != "" {
		argv = append(argv, effortFlag, p.effort)
	}
	if sessionID != "" {
		argv = append(argv, resumeFlag, sessionID)
	}
	return append(argv, prompt)
}

// PrintArgs returns the executable and arguments for a fresh turn.
func (p *provider) PrintArgs(prompt string) []string {
	return p.args("", prompt)
}

// ResumeArgs returns the executable and arguments for an existing session.
func (p *provider) ResumeArgs(sessionID, prompt string) []string {
	return p.args(sessionID, prompt)
}

func (p *provider) InteractiveArgs(_ string) []string {
	args := []string{"cmd"}
	if p.model != "" {
		args = append(args, modelFlag, p.model)
	}
	return args
}

func (p *provider) ParseStreamLine(line string) []Event {
	return p.parser.parseLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
