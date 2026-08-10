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
	return p.printCommand("", prompt)
}

func (p *provider) ResumeCommand(sessionID, prompt string) string {
	return p.printCommand(sessionID, prompt)
}

func (p *provider) printCommand(sessionID, prompt string) string {
	modelFlag := ""
	if p.model != "" {
		modelFlag = " --model " + harness.ShellEscape(p.model)
	}
	sessionFlag := ""
	if sessionID != "" {
		sessionFlag = " --session " + harness.ShellEscape(sessionID)
	}
	// Pi saves print-mode sessions by default. Fresh runs must remain
	// persistent so the session ID emitted by the JSON stream can be resumed.
	return fmt.Sprintf(
		"pi -p --mode json%s%s %s",
		modelFlag,
		sessionFlag,
		harness.ShellEscape(prompt),
	)
}

func (p *provider) InteractiveArgs(_ string) []string {
	args := []string{"pi"}
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
