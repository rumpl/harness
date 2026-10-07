package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type commandProvider struct {
	command string
}

func (p commandProvider) Name() string                        { return "test" }
func (p commandProvider) PrintCommand(string) string          { return p.command }
func (p commandProvider) ResumeCommand(string, string) string { return p.command }
func (p commandProvider) InteractiveArgs(string) []string     { return nil }
func (p commandProvider) ParseStreamLine(line string) []Event {
	return []Event{{Type: EventText, Text: line}}
}

func TestCommandLargeLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	for _, resume := range []bool{false, true} {
		text := strings.Repeat("界", 100000)
		path := filepath.Join(t.TempDir(), "output")
		if err := os.WriteFile(path, []byte(text+"\r\nlast"), 0o600); err != nil {
			t.Fatal(err)
		}
		p := commandProvider{command: "cat " + ShellEscape(path)}
		var got []string
		handle := func(ev Event) { got = append(got, ev.Text) }
		var err error
		if resume {
			err = Resume(t.Context(), p, "session", "prompt", handle)
		} else {
			err = Run(t.Context(), p, "prompt", handle)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0] != text || got[1] != "last" {
			t.Fatalf("incorrect output: %d lines", len(got))
		}
	}
}

func TestLineStream(t *testing.T) {
	t.Run("chunked UTF-8 and final line", func(t *testing.T) {
		var got []string
		stream := lineStream{handle: func(line string) error {
			got = append(got, line)
			return nil
		}}
		for _, b := range []byte("界\nlast") {
			if err := stream.write([]byte{b}); err != nil {
				t.Fatal(err)
			}
		}
		if err := stream.finish(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0] != "界" || got[1] != "last" {
			t.Fatalf("output = %q", got)
		}
	})
	t.Run("exact bound", func(t *testing.T) {
		stream := lineStream{handle: func(line string) error {
			if len(line) != maxStreamLineBytes {
				t.Fatalf("line size = %d", len(line))
			}
			return nil
		}}
		if err := stream.write([]byte(strings.Repeat("x", maxStreamLineBytes) + "\n")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("oversize without newline", func(t *testing.T) {
		stream := lineStream{handle: func(string) error {
			t.Fatal("oversized line delivered")
			return nil
		}}
		if err := stream.write([]byte(strings.Repeat("x", maxStreamLineBytes))); err != nil {
			t.Fatal(err)
		}
		if err := stream.write([]byte("x")); err == nil {
			t.Fatal("expected size error")
		}
	})
}

func TestCommandFailureAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	t.Run("exit status", func(t *testing.T) {
		err := Run(t.Context(), commandProvider{"exit 2"}, "", func(Event) {})
		if err == nil || !strings.Contains(err.Error(), "exit status 2") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("canceled before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := Run(ctx, commandProvider{"sleep 60"}, "", func(Event) {})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("slow callback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		var got []string
		err := Run(ctx, commandProvider{"printf 'first\nlast\n'"}, "", func(ev Event) {
			got = append(got, ev.Text)
			if ev.Text == "first" {
				time.Sleep(streamDrainTimeout + 100*time.Millisecond)
			}
		})
		if err != nil || len(got) != 2 {
			t.Fatalf("error = %v, output = %q", err, got)
		}
	})
}

type argsProvider struct {
	commandProvider

	args []string
}

func (p argsProvider) PrintArgs(string) []string          { return p.args }
func (p argsProvider) ResumeArgs(string, string) []string { return p.args }

func TestEmptyCommandArgs(t *testing.T) {
	for _, args := range [][]string{nil, {""}} {
		p := argsProvider{args: args}
		if err := Run(t.Context(), p, "", func(Event) {}); err == nil || !strings.Contains(err.Error(), "executable is empty") {
			t.Fatalf("Run error = %v", err)
		}
		if err := Resume(t.Context(), p, "session", "", func(Event) {}); err == nil || !strings.Contains(err.Error(), "executable is empty") {
			t.Fatalf("Resume error = %v", err)
		}
	}
}
