package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"

	"github.com/filebrowser/filebrowser/v2/settings"
)

// ErrOutputLimit indicates that a command exceeded the configured output cap.
var ErrOutputLimit = errors.New("command output exceeds sandbox limit")

// NewCommand creates a command through the configured sandbox. When the
// sandbox is enabled, configuration is validated on every execution and the
// target is always appended after the launcher delimiter; there is no raw
// execution fallback.
func NewCommand(s *settings.Settings, target []string) (*exec.Cmd, context.CancelFunc, error) {
	if len(target) == 0 || target[0] == "" {
		return nil, nil, errors.New("empty command")
	}

	sandbox := s.ExecutionSandbox
	if !sandbox.Enabled {
		return nil, nil, errors.New("execution sandbox must be enabled before commands or hooks can run")
	}
	if err := sandbox.Validate(); err != nil {
		return nil, nil, err
	}

	command := target
	if sandbox.Enabled {
		command = append(append([]string{}, sandbox.Command...), target...)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if sandbox.Enabled {
		ctx, cancel = context.WithTimeout(context.Background(), sandbox.TimeoutDuration())
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	return exec.CommandContext(ctx, command[0], command[1:]...), cancel, nil
}

// LimitOutput caps output only when the sandbox is enabled. It is deliberately
// applied at every execution boundary so commands cannot turn an otherwise
// bounded sandbox into an unbounded memory, WebSocket, or log-output sink.
func LimitOutput(s *settings.Settings, w io.Writer) io.Writer {
	if !s.ExecutionSandbox.Enabled {
		return w
	}
	return &limitedWriter{w: w, remaining: s.ExecutionSandbox.OutputLimit()}
}

type limitedWriter struct {
	mu        sync.Mutex
	w         io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.remaining <= 0 {
		return 0, ErrOutputLimit
	}
	exceeded := int64(len(p)) > w.remaining
	if exceeded {
		p = p[:w.remaining]
	}
	n, err := w.w.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if exceeded {
		return n, ErrOutputLimit
	}
	return n, nil
}
