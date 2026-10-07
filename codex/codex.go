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
	return p.printCommand("", prompt)
}

func (p *provider) ResumeCommand(sessionID, prompt string) string {
	return p.printCommand(sessionID, prompt)
}

func (p *provider) printCommand(sessionID, prompt string) string {
	modelFlag := ""
	if p.model != "" {
		modelFlag = " -m " + harness.ShellEscape(p.model)
	}
	verb := "codex exec"
	if sessionID != "" {
		verb += " resume " + harness.ShellEscape(sessionID)
	}
	return fmt.Sprintf(
		"%s --json --dangerously-bypass-approvals-and-sandbox%s %s",
		verb,
		modelFlag,
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
	args := []string{"codex", "exec"}
	if sessionID != "" {
		args = append(args, "resume", sessionID)
	}
	args = append(args, "--json", "--dangerously-bypass-approvals-and-sandbox")
	if p.model != "" {
		args = append(args, "-m", p.model)
	}
	return append(args, prompt)
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
