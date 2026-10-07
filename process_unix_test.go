//go:build unix

package harness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDescendantCleanup(t *testing.T) {
	for _, tt := range []struct {
		name, script, want string
		cancel             bool
	}{
		{name: "cancellation", script: "sleep 60 &\necho $! > PID\necho ready\nwait", cancel: true},
		{name: "inherited stdout", script: "sleep 60 &\necho $! > PID\necho ready", want: "stdout remained open"},
		{name: "active stdout", script: "(while :; do echo ready; sleep 0.1; done) &\necho $! > PID", want: "stdout remained open"},
		{name: "oversized unterminated line", script: "sleep 60 &\necho $! > PID\nhead -c 16777217 /dev/zero\nwait", want: "stream line exceeds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pidPath := filepath.Join(t.TempDir(), "pid")
			script := strings.ReplaceAll(tt.script, "PID", ShellEscape(pidPath))
			var pid int
			t.Cleanup(func() {
				if pid > 0 {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err := Run(ctx, commandProvider{script}, "", func(Event) {
				if tt.cancel {
					cancel()
				}
			})
			if tt.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
				if ctx.Err() != nil {
					t.Fatal("watchdog expired")
				}
			}
			data, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatal(err)
			}
			pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for syscall.Kill(pid, 0) == nil {
				// Linux may retain a killed descendant as a zombie until reaped.
				out, err := exec.CommandContext(t.Context(), "ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
				if err != nil || strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("descendant %d still running", pid)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}
