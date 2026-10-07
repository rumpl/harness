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
	return p.printCommand("", prompt)
}

func (p *provider) ResumeCommand(sessionID, prompt string) string {
	return p.printCommand(sessionID, prompt)
}

func (p *provider) printCommand(sessionID, prompt string) string {
	sessionFlag := ""
	if sessionID != "" {
		sessionFlag = " --session " + harness.ShellEscape(sessionID)
	}
	return fmt.Sprintf(
		"docker-agent run --json --exec --yolo%s %s %s",
		sessionFlag,
		harness.ShellEscape(p.image),
		harness.ShellEscape(prompt),
	)
}

// PrintArgs returns the executable and arguments for a fresh turn.
func (p *provider) PrintArgs(prompt string) []string {
	return p.printArgs("", prompt)
}

// ResumeArgs returns the executable and arguments for an existing session.
func (p *provider) ResumeArgs(sessionID, prompt string) []string {
	return p.printArgs(sessionID, prompt)
}

func (p *provider) printArgs(sessionID, prompt string) []string {
	args := []string{"docker-agent", "run", "--json", "--exec", "--yolo"}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	return append(args, p.image, prompt)
}

func (p *provider) InteractiveArgs(_ string) []string {
	return []string{"docker-agent", "run", "--yolo", p.image}
}

func (p *provider) ParseStreamLine(line string) []Event {
	return parseStreamLine(line)
}

// Event is an alias for [harness.Event].
type Event = harness.Event
