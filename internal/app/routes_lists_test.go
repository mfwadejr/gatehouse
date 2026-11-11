package app

import (
	"encoding/json"
	"strings"
	"testing"

	"nopewarden/internal/fw"
)

func saveRouteWithList(t *testing.T, a *App, list string) {
	t.Helper()
	tx, err := a.Config.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SavePortList(tx, fw.PortListRow{Name: list, Ports: []string{"tcp/32400"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.SaveRoute(tx, fw.RouteRow{Name: "Plex out", Enabled: true, TargetKind: "links", Target: "LTE,Fibre", Mode: "balance",
		Who: "net:Home", Service: "list:" + list}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func routeService(t *testing.T, a *App) string {
	t.Helper()
	var s string
	if err := a.Config.QueryRow(`SELECT service FROM routes WHERE name='Plex out'`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Renaming a service list makes the routes that use it follow the new name.
func TestRenamingServiceListUpdatesRoutes(t *testing.T) {
	a, _ := newSpeedApp(t)
	saveRouteWithList(t, a, "Plex")
	lists, _ := fw.PortLists(a.Config)
	tx, _ := a.Config.Begin()
	if _, err := fw.SavePortList(tx, fw.PortListRow{ID: lists[0].ID, Name: "Media", Ports: lists[0].Ports}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if got := routeService(t, a); got != "list:Media" {
		t.Fatalf("route service = %q, want list:Media", got)
	}
}

// A list a route uses cannot be deleted, and the message names the route.
func TestDeletingServiceListUsedByRouteIsRefused(t *testing.T) {
	a, _ := newSpeedApp(t)
	saveRouteWithList(t, a, "Plex")
	lists, _ := fw.PortLists(a.Config)
	tx, _ := a.Config.Begin()
	defer tx.Rollback()
	err := fw.DeletePortList(tx, lists[0].ID)
	if err == nil || !strings.Contains(err.Error(), "Plex out") {
		t.Fatalf("want refusal naming the route, got %v", err)
	}
}

// A route whose list has gone matches nothing, and the screen says it needs attention.
func TestRouteWithMissingListSendsNothingAndNeedsAttention(t *testing.T) {
	a, _ := newSpeedApp(t)
	saveRouteWithList(t, a, "Plex")
	if _, err := a.Config.Exec(`DELETE FROM fw_objects WHERE kind='ports'`); err != nil {
		t.Fatal(err)
	}
	if m := fw.MissingServiceLists("list:Plex", map[string][]string{}); len(m) != 1 || m[0] != "Plex" {
		t.Fatalf("missing = %v", m)
	}
	if len(fw.MissingServiceLists("list:Plex,tcp/80", map[string][]string{"Plex": {"tcp/1"}})) != 0 {
		t.Fatal("present list reported missing")
	}
	v := a.routesView()
	raw, _ := json.Marshal(v)
	b := string(raw)
	if !strings.Contains(b, `"attention"`) || !strings.Contains(b, "no longer exists") {
		t.Fatalf("view lacks needs-attention state: %s", b)
	}
}
