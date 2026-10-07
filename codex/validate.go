package codex

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/rumpl/harness"
)

// NewStreamValidator creates independent turn state for each Run or Resume.
func (p *provider) NewStreamValidator() harness.StreamValidator {
	return &turnValidator{}
}

type turnValidator struct {
	completed bool
}

func (v *turnValidator) ValidateStreamLine(line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// JSON decoder errors can quote raw input; never expose stream contents.
		return errors.New("invalid Codex JSON event")
	}
	if event.Type == "" {
		return errors.New("invalid Codex JSON event: missing event type")
	}
	switch event.Type {
	case "turn.started":
		v.completed = false
	case "turn.completed":
		v.completed = true
	case "turn.failed":
		return errors.New("codex turn failed")
	}
	// Error events can be transient reconnection warnings. Unknown typed
	// events are ignored for compatibility with newer CLI versions.
	return nil
}

func (v *turnValidator) EndStream() error {
	if !v.completed {
		return errors.New("codex stream ended without turn.completed")
	}
	return nil
}
