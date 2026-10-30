package app

import "testing"

func TestRestorePointBeforeApplyAndRevert(t *testing.T) {
	c, _, _ := siteBox(t, "192.168.1.1/24")
	a := c.app
	if a == nil {
		t.Skip("box has no app")
	}
	if rc, _, raw := c.call("POST", "/api/fw/apply", map[string]string{"code": ""}); rc != 200 {
		t.Skipf("apply not possible in this box: %d %s", rc, raw)
	}
	pts := a.changeList()
	if len(pts) != 1 || !pts[0].OK {
		t.Fatalf("restore points: %+v", pts)
	}
	// Change something, then revert to the point.
	a.Config.SetSetting("fw.block_dot", "0")
	rc, _, raw := c.call("POST", "/api/changes/revert", map[string]string{"name": pts[0].Name, "password": "correct horse battery"})
	if rc != 200 {
		t.Fatalf("%d %s", rc, raw)
	}
	if rc, _, _ = c.call("POST", "/api/changes/revert", map[string]string{"name": "manual-20260101-000000.ghbk"}); rc == 200 {
		t.Fatal("reverted to something that is not a restore point")
	}
	if len(a.changeList()) != 1 {
		t.Fatal("list changed")
	}
}
