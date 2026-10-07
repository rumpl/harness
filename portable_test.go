package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rumpl/harness"
	"github.com/rumpl/harness/claudecode"
	"github.com/rumpl/harness/codex"
	"github.com/rumpl/harness/dockeragent"
	"github.com/rumpl/harness/opencode"
	"github.com/rumpl/harness/pi"
)

// TestMain lets this test binary stand in for a native CLI on every OS.
func TestMain(m *testing.M) {
	if os.Getenv("HARNESS_NATIVE_HELPER") == "1" {
		args, err := json.Marshal(os.Args[1:])
		if err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("HARNESS_ARGS_PATH"), args, 0o600); err != nil {
			os.Exit(3)
		}
		output, err := os.ReadFile(os.Getenv("HARNESS_OUTPUT_PATH"))
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.Write(output)
		switch os.Getenv("HARNESS_MODE") {
		case "wait":
			time.Sleep(time.Minute)
		case "eof":
			_ = os.Stdout.Close()
			time.Sleep(time.Minute)
		case "exit":
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func nativeCLI(t *testing.T, binary, output string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, binary), data, 0o700); err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(dir, "args.json")
	// No sh, cat, or other external programs are available on this PATH.
	t.Setenv("PATH", dir)
	t.Setenv("HARNESS_NATIVE_HELPER", "1")
	t.Setenv("HARNESS_ARGS_PATH", argsPath)
	outputPath := filepath.Join(dir, "output")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_OUTPUT_PATH", outputPath)
	t.Setenv("HARNESS_MODE", "")
	return argsPath
}

func TestNativeProviders(t *testing.T) {
	model := "model with spaces' & %PATH% $(unsafe) 界"
	prompt := "prompt with spaces' \"quotes\" & %PATH% $(unsafe) 界\nnext line"
	session := "session' & %PATH% $(unsafe) 界"
	for _, tt := range []struct {
		name, binary, output string
		provider             harness.Provider
		print, resume        []string
	}{
		{
			"codex", "codex", "{\"type\":\"thread.started\",\"thread_id\":\"thread\"}\n{\"type\":\"turn.completed\"}\n", codex.New(model),
			[]string{"codex", "exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-m", model, prompt},
			[]string{"codex", "exec", "resume", session, "--json", "--dangerously-bypass-approvals-and-sandbox", "-m", model, prompt},
		},
		{
			"claude", "claude", "{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"thread\"}\n", claudecode.New(model, claudecode.WithEffort(claudecode.EffortHigh)),
			[]string{"claude", "--print", "--verbose", "--dangerously-skip-permissions", "--include-partial-messages", "--output-format", "stream-json", "--model", model, "--effort", "high", "-p", prompt},
			[]string{"claude", "--print", "--verbose", "--dangerously-skip-permissions", "--include-partial-messages", "--output-format", "stream-json", "--model", model, "--effort", "high", "--resume", session, "-p", prompt},
		},
		{
			"pi", "pi", "{\"type\":\"session\",\"id\":\"thread\"}\n", pi.New(model),
			[]string{"pi", "-p", "--mode", "json", "--model", model, prompt},
			[]string{"pi", "-p", "--mode", "json", "--model", model, "--session", session, prompt},
		},
		{
			"docker-agent", "docker-agent", "{\"type\":\"stream_started\",\"session_id\":\"thread\"}\n", dockeragent.New(model),
			[]string{"docker-agent", "run", "--json", "--exec", "--yolo", model, prompt},
			[]string{"docker-agent", "run", "--json", "--exec", "--yolo", "--session", session, model, prompt},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cp := tt.provider.(harness.ResumableCommandProvider)
			if !reflect.DeepEqual(cp.PrintArgs(prompt), tt.print) || !reflect.DeepEqual(cp.ResumeArgs(session, prompt), tt.resume) {
				t.Fatal("incorrect command arguments")
			}
			argsPath := nativeCLI(t, tt.binary, tt.output)
			for _, resume := range []bool{false, true} {
				var gotSession string
				handle := func(ev harness.Event) {
					if ev.Type == harness.EventSessionID {
						gotSession = ev.SessionID
					}
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				var err error
				want := tt.print[1:]
				if resume {
					err = harness.Resume(ctx, cp, session, prompt, handle)
					want = tt.resume[1:]
				} else {
					err = harness.Run(ctx, cp, prompt, handle)
				}
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				if gotSession != "thread" {
					t.Fatalf("session = %q", gotSession)
				}
				data, err := os.ReadFile(argsPath)
				if err != nil {
					t.Fatal(err)
				}
				var args []string
				if err := json.Unmarshal(data, &args); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(args, want) {
					t.Fatalf("args = %q, want %q", args, want)
				}
			}
		})
	}
}

func TestNativeCodexFailures(t *testing.T) {
	for _, tt := range []struct{ name, output, mode, want string }{
		{"failed", "{\"type\":\"turn.failed\"}\n", "wait", "codex turn failed"},
		{"malformed", "{broken\n", "wait", "invalid Codex JSON event"},
		{"early EOF", "", "eof", "without turn.completed"},
		{"exit", "{\"type\":\"turn.completed\"}\n", "exit", "exit status 2"},
		{"oversized", strings.Repeat("x", 16*1024*1024+1), "wait", "stream line exceeds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nativeCLI(t, "codex", tt.output)
			t.Setenv("HARNESS_MODE", tt.mode)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err := harness.Run(ctx, codex.New(""), "prompt", func(harness.Event) {})
			if err == nil || !strings.Contains(err.Error(), tt.want) || ctx.Err() != nil {
				t.Fatalf("error = %v, context = %v", err, ctx.Err())
			}
		})
	}
}

func TestNativeCancellation(t *testing.T) {
	nativeCLI(t, "codex", "{\"type\":\"thread.started\",\"thread_id\":\"thread\"}\n")
	t.Setenv("HARNESS_MODE", "wait")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := harness.Run(ctx, codex.New(""), "prompt", func(ev harness.Event) {
		if ev.Type == harness.EventSessionID {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenCodeCommandArgs(t *testing.T) {
	p := opencode.New("provider/model", opencode.WithAgent("agent name"), opencode.WithThinking()).(harness.ResumableCommandProvider)
	want := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "--model", "provider/model", "--agent", "agent name", "--thinking", "--session", "session", "prompt"}
	if got := p.ResumeArgs("session", "prompt"); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q", got)
	}
}
