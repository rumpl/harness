// Package dockeragent provides a [harness.Provider] implementation for the
// Docker Agent CLI.
package dockeragent

import (
	"fmt"

	"github.com/rumpl/harness"
)

type provider struct {
	image string
}

// New creates a Docker Agent [harness.Provider] for the given image.
func New(image string) harness.Provider {
	return &provider{image: image}
}

func (p *provider) Name() string { return "docker-agent" }

func (p *provider) PrintCommand(prompt string) string {
	return fmt.Sprintf(
		"docker-agent run --json --exec --yolo %s %s",
		harness.ShellEscape(p.image),
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	return []string{"docker-agent", "run", "--yolo", p.image}
}

func (p *provider) ParseStreamLine(line string) []Event {
	return parseStreamLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
