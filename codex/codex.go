// Package codex provides a [harness.Provider] implementation for the OpenAI
// Codex CLI agent.
package codex

import (
	"fmt"

	"github.com/rumpl/harness"
)

type provider struct {
	model string
}

// New creates a Codex [harness.Provider] for the given model.
func New(model string) harness.Provider {
	return &provider{model: model}
}

func (p *provider) Name() string { return "codex" }

func (p *provider) PrintCommand(prompt string) string {
	modelFlag := ""
	if p.model != "" {
		modelFlag = fmt.Sprintf(" -m %s", harness.ShellEscape(p.model))
	}
	return fmt.Sprintf(
		"codex exec --json --dangerously-bypass-approvals-and-sandbox%s %s",
		modelFlag,
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	args := []string{"codex"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	return args
}

func (p *provider) ParseStreamLine(line string) []Event {
	return parseStreamLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
