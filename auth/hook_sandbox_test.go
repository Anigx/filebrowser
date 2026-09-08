package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/filebrowser/filebrowser/v2/settings"
)

func TestHookAuthFailsClosedWhenSandboxIsInvalid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell")
	}

	marker := filepath.Join(t.TempDir(), "auth-hook-ran")
	script := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\necho hook.action=auth\n"), 0o700); err != nil {
		t.Fatalf("write hook script: %v", err)
	}

	a := &HookAuth{
		Command: script,
		Settings: &settings.Settings{
			ExecutionSandbox: settings.ExecutionSandbox{Enabled: true},
		},
	}
	if _, err := a.RunCommand(); err == nil {
		t.Fatal("invalid enabled sandbox must reject auth hook")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("auth hook escaped the invalid sandbox: stat marker = %v", err)
	}
}
