package netd

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"time"
)

// sysRun runs a command with a time limit and returns what it printed. It is a variable so tests
// can stand in for the real tools.
var sysRun = func(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), errors.New("timed out")
	}
	return string(out), err
}

// sys is where the kernel's sysfs lives (moved under Root in tests).
func (s *Service) sys() string {
	if s.Root != "" {
		return filepath.Join(s.Root, "sys")
	}
	return "/sys"
}
