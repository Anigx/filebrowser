package runner

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/filebrowser/filebrowser/v2/settings"
	"github.com/filebrowser/filebrowser/v2/users"
)

func TestNewCommandSandboxFailsClosedWithoutLauncher(t *testing.T) {
	_, cancel, err := NewCommand(&settings.Settings{
		ExecutionSandbox: settings.ExecutionSandbox{Enabled: true},
	}, []string{"/bin/echo", "must-not-run"})
	if cancel != nil {
		cancel()
	}
	if err == nil {
		t.Fatal("enabled sandbox without a launcher must reject execution")
	}
}

func TestNewCommandSandboxWrapsTargetWithoutRawFallback(t *testing.T) {
	s := &settings.Settings{
		ExecutionSandbox: settings.ExecutionSandbox{
			Enabled: true,
			Command: []string{"/sandbox/launcher", "--policy", "/etc/filebrowser/policy", "--"},
		},
	}

	cmd, cancel, err := NewCommand(s, []string{"/bin/echo", "hello"})
	if err != nil {
		t.Fatalf("NewCommand returned error: %v", err)
	}
	defer cancel()

	if got, want := cmd.Path, "/sandbox/launcher"; got != want {
		t.Fatalf("command path = %q, want sandbox launcher %q", got, want)
	}
	if got, want := strings.Join(cmd.Args, "\x00"), "/sandbox/launcher\x00--policy\x00/etc/filebrowser/policy\x00--\x00/bin/echo\x00hello"; got != want {
		t.Fatalf("sandbox args = %q, want %q", got, want)
	}
}

func TestNewCommandRejectsUnsafeSandboxConfiguration(t *testing.T) {
	tests := []settings.ExecutionSandbox{
		{Enabled: true, Command: []string{"sandbox", "--"}},
		{Enabled: true, Command: []string{"/sandbox/launcher"}},
		{Enabled: true, Command: []string{"/sandbox/launcher", "--"}, Timeout: "0s"},
	}
	for _, sandbox := range tests {
		t.Run(strings.Join(sandbox.Command, " ")+sandbox.Timeout, func(t *testing.T) {
			_, cancel, err := NewCommand(&settings.Settings{ExecutionSandbox: sandbox}, []string{"/bin/true"})
			if cancel != nil {
				cancel()
			}
			if err == nil {
				t.Fatal("unsafe sandbox configuration was accepted")
			}
		})
	}
}

func TestLimitOutputFailsAtConfiguredLimit(t *testing.T) {
	w := LimitOutput(&settings.Settings{ExecutionSandbox: settings.ExecutionSandbox{
		Enabled:        true,
		Command:        []string{"/sandbox/launcher", "--"},
		MaxOutputBytes: 3,
	}}, io.Discard)

	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatalf("write at limit returned error: %v", err)
	}
	if _, err := w.Write([]byte("d")); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("write above limit error = %v, want ErrOutputLimit", err)
	}
}

func TestEventHookFailsClosedWhenSandboxIsInvalid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	marker := filepath.Join(t.TempDir(), "event-ran")
	script := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatalf("write hook script: %v", err)
	}

	r := &Runner{Enabled: true, Settings: &settings.Settings{
		ExecutionSandbox: settings.ExecutionSandbox{Enabled: true},
	}}
	if err := r.exec(script, "after_upload", "", "", &users.User{}); err == nil {
		t.Fatal("invalid enabled sandbox must reject event hook")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("event hook escaped the invalid sandbox: stat marker = %v", err)
	}
}
