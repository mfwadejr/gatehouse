package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gatehouse/internal/fw"
)

// ResetMarker is left in the data directory by the recovery screen, which can clear the settings
// but not talk to the running programs. The next start puts the empty network and the safe
// firewall in place.
const ResetMarker = "reset-pending"

// firstBoot applies the network and firewall that were chosen in the installer. Nothing was
// running before this, so there is no earlier state to return to and no one to confirm it:
// the change is applied and confirmed straight away.
func (a *App) firstBoot(ctx context.Context) {
	if marker := filepath.Join(a.Cfg.DataDir, ResetMarker); fileExists(marker) {
		for i := 0; a.Net != nil && i < 30; i++ {
			if _, err := a.Net.WanStatus(); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		a.backToStart()
		os.Remove(marker)
		a.publishNet()
		a.publishFW()
		return
	}
	if a.Config.Setting("net.autoapply", "") != "1" || a.Net == nil || a.FW == nil {
		return
	}
	// Give the privileged helper a moment to come up.
	for i := 0; ; i++ {
		if _, err := a.Net.WanStatus(); err == nil {
			break
		}
		if i >= 30 {
			log.Printf("first boot: the network helper is not answering; leaving setup for the console")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	if err := a.applyStored(); err != nil {
		log.Printf("first boot: %v", err)
		return
	}
	a.Config.SetSetting("net.autoapply", "0")
	a.DHCP.Reload()
	a.publishNet()
	a.publishFW()
	log.Printf("first boot: network and firewall from the installer are in place")
}

func (a *App) applyStored() error {
	p, err := a.buildPlan(a.Config)
	if err == nil {
		err = a.validatePlan(p)
	}
	if err != nil {
		return err
	}
	tok, _, err := a.Net.NetApply(*p, confirmWindow)
	if err != nil {
		return err
	}
	if err := a.Net.NetConfirm(tok); err != nil {
		return err
	}
	a.Config.SetSetting("net.applied", fingerprint(p))
	a.Config.SetSetting("net.applied_base", baseFingerprint(p))

	m, script, err := a.currentRuleset()
	if err != nil {
		return err
	}
	ftok, _, err := a.FW.Apply(script, confirmWindow, "setup")
	if err != nil {
		return err
	}
	if err := a.FW.Confirm(ftok); err != nil {
		return err
	}
	summary := strings.Join(fw.Describe(m), " ")
	if len(summary) > 500 {
		summary = summary[:500]
	}
	a.Config.Exec(`INSERT INTO fw_revisions(created_at,author,summary,ruleset,status) VALUES(?,?,?,?,'confirmed')`, time.Now().Unix(), "setup", summary, script)
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
