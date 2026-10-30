package netd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var reDiagTarget = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)

// DiagTargetOK says whether a name or address is safe to hand to ping or traceroute. A leading
// dash, spaces and anything unusual are refused so nothing can pass as an option.
func DiagTargetOK(t string) bool { return reDiagTarget.MatchString(t) }

// Diag runs one network check from this machine and returns its output as text. Only ping and
// traceroute exist, with fixed options.
func (s *Service) Diag(kind, target string) (string, error) {
	target = strings.TrimSpace(target)
	if !DiagTargetOK(target) {
		return "", errors.New("enter a website name or an address, such as example.com or 8.8.8.8")
	}
	var cmd *exec.Cmd
	limit := 45 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	switch kind {
	case "ping":
		cmd = exec.CommandContext(ctx, "ping", "-n", "-c", "4", "-W", "2", "--", target)
	case "trace":
		cmd = exec.CommandContext(ctx, "traceroute", "-n", "-w", "2", "-q", "1", "-m", "20", "--", target)
	default:
		return "", errors.New("unknown check")
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if out == "" && err != nil {
		return "", errors.New("the check could not run: " + err.Error())
	}
	if ctx.Err() != nil {
		out += "\n(stopped after 45 seconds)"
	}
	return out, nil
}
