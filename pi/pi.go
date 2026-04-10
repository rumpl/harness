// Package pi provides a [harness.Provider] implementation for the Pi CLI agent.
package pi

import (
	"fmt"

	"github.com/rumpl/harness"
)

type provider struct {
	model string
}

// New creates a Pi [harness.Provider] for the given model.
func New(model string) harness.Provider {
	return &provider{model: model}
}

func (p *provider) Name() string { return "pi" }

func (p *provider) PrintCommand(prompt string) string {
	return fmt.Sprintf(
		"pi -p --mode json --no-session --model %s %s",
		harness.ShellEscape(p.model),
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	return []string{"pi", "--model", p.model}
}

func (p *provider) ParseStreamLine(line string) []Event {
	return parseStreamLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
