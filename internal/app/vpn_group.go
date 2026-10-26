package app

import "strings"

// vpnFollowGroup puts the VPN routes into effect again after a device group changed, when a VPN sends
// that group's devices through a tunnel. Without it the routes keep the old members until the VPN
// happens to be saved again. It returns a plain sentence when the new members could not be applied
// right now; the group itself is saved either way.
func (a *App) vpnFollowGroup(name, author string) string {
	var n int
	a.Config.QueryRow(`SELECT COUNT(*) FROM vpn_routes WHERE kind='group' AND value=?`, name).Scan(&n)
	if n == 0 {
		return ""
	}
	if a.Net == nil || a.FW == nil {
		return ""
	}
	if err := a.applyVPN(author); err != nil {
		msg := strings.TrimSuffix(err.Error(), ".")
		return "The group is saved, but the VPN could not use the new members yet: " + msg + ". It will use them the next time the VPN is applied."
	}
	return ""
}
