package netcfg

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// StartWatchdogProcess launches "gatehouse rollback -kind net" detached from the caller.
func StartWatchdogProcess(dir string, deadline time.Time, token string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"rollback", "-kind", "net", "-dir", dir, "-token", token, "-deadline", fmt.Sprint(deadline.Unix())}
	if _, err := exec.LookPath("systemd-run"); err == nil && os.Getenv("GATEHOUSE_NO_SYSTEMD") == "" {
		unit := "gatehouse-net-rollback"
		exec.Command("systemctl", "stop", unit+".service").Run()
		if err := exec.Command("systemd-run", append([]string{"--unit=" + unit, "--collect",
			fmt.Sprintf("--on-active=%ds", int(time.Until(deadline).Seconds())+1), self}, args...)...).Run(); err == nil {
			return nil
		}
	}
	c := exec.Command(self, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}
