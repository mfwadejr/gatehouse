package app

import (
	"strings"
	"testing"
	"time"

	dnssrv "nopewarden/internal/dns"
)

func TestOneBlockHoldsManyThingsAndPlaces(t *testing.T) {
	a := newScopeApp(t)
	id, err := a.saveBlockSet(blockSet{Name: "Bedtime", Enabled: true,
		Items:   []blockItem{{Kind: "app", Value: "youtube"}, {Kind: "name", Value: "Games.Example.com"}},
		Targets: []blockTarget{{Scope: "device", Target: "AA:AA:AA:AA:AA:01"}, {Scope: "profile", Target: "3"}}})
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := a.loadScopeRules(); len(rows) != 4 {
		t.Fatalf("want 2 things x 2 places = 4 rows, got %d", len(rows))
	}
	kid := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:01", GroupID: 2, Filtering: true}
	teen := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:09", GroupID: 3, Filtering: true}
	other := dnssrv.ClientInfo{MAC: "aa:aa:aa:aa:aa:77", GroupID: 4, Filtering: true}
	if a.scopedBlock("1.1.1.1", kid, "r4.googlevideo.com") == "" || a.scopedBlock("1.1.1.1", teen, "cdn.games.example.com") == "" {
		t.Fatal("both places should be blocked, for both things")
	}
	if a.scopedBlock("1.1.1.1", other, "r4.googlevideo.com") != "" {
		t.Fatal("a device outside the block must not be blocked")
	}
	// Pause and turn off work on the whole block.
	until := time.Now().Add(time.Hour).Unix()
	a.Config.Exec(`UPDATE block_sets SET paused_until=? WHERE id=?`, until, id)
	a.Config.Exec(`UPDATE scope_rules SET paused_until=? WHERE set_id=?`, until, id)
	a.dropScopeCache()
	if a.scopedBlock("1.1.1.1", kid, "r4.googlevideo.com") != "" {
		t.Fatal("paused block still blocks")
	}
	// Editing keeps the number and rebuilds the rows.
	if _, err := a.saveBlockSet(blockSet{ID: id, Name: "Bedtime", Enabled: true,
		Items: []blockItem{{Kind: "app", Value: "YouTube"}}, Targets: []blockTarget{{Scope: "everyone"}}}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := a.loadScopeRules(); len(rows) != 1 || rows[0].Scope != "everyone" {
		t.Fatalf("edit did not rebuild the rows: %+v", rows)
	}
	v := a.blockViews()
	if len(v) != 1 || v[0].Title != "Bedtime" || v[0].Where != "Everyone" {
		t.Fatalf("view: %+v", v)
	}
}

func TestBlockNameIsMadeWhenEmptyAndInputIsChecked(t *testing.T) {
	a := newScopeApp(t)
	if _, err := a.saveBlockSet(blockSet{Enabled: true, Items: []blockItem{{Kind: "app", Value: "youtube"}}, Targets: []blockTarget{{Scope: "everyone"}}}); err != nil {
		t.Fatal(err)
	}
	if v := a.blockViews(); !strings.HasPrefix(v[0].Title, "Block YouTube") {
		t.Fatalf("auto name: %q", v[0].Title)
	}
	bad := []blockSet{
		{Enabled: true, Targets: []blockTarget{{Scope: "everyone"}}},
		{Enabled: true, Items: []blockItem{{Kind: "app", Value: "youtube"}}},
		{Enabled: true, Items: []blockItem{{Kind: "app", Value: "nosuchapp"}}, Targets: []blockTarget{{Scope: "everyone"}}},
		{Enabled: true, Items: []blockItem{{Kind: "app", Value: "youtube"}}, Targets: []blockTarget{{Scope: "device"}}},
		{Enabled: true, Name: strings.Repeat("x", 61), Items: []blockItem{{Kind: "app", Value: "youtube"}}, Targets: []blockTarget{{Scope: "everyone"}}},
	}
	for i, b := range bad {
		if _, err := a.saveBlockSet(b); err == nil {
			t.Fatalf("bad block %d was accepted", i)
		}
	}
}

func TestOldSingleBlocksBecomeBlocksOfOne(t *testing.T) {
	a := newScopeApp(t)
	a.Config.Exec(`INSERT INTO scope_rules(id,kind,value,scope,target,allow,method,schedule,enabled,created_at) VALUES(7,'name','games.example.com','profile','2',0,'fast','',1,1)`)
	a.migrateBlockSets()
	a.migrateBlockSets() // running it again changes nothing
	v := a.blockViews()
	if len(v) != 1 || v[0].ID != 7 || len(v[0].Items) != 1 || v[0].Targets[0].Scope != "profile" {
		t.Fatalf("migration: %+v", v)
	}
	var n int
	a.Config.QueryRow(`SELECT count(*) FROM scope_rules WHERE set_id=7`).Scan(&n)
	if n != 1 {
		t.Fatalf("the old row should now belong to block 7, got %d", n)
	}
}

func TestRemovingAProfileTakesItOutOfBlocks(t *testing.T) {
	a := newScopeApp(t)
	var gid int64
	a.Config.QueryRow(`SELECT id FROM groups LIMIT 1`).Scan(&gid)
	id, err := a.saveBlockSet(blockSet{Enabled: true, Items: []blockItem{{Kind: "app", Value: "youtube"}},
		Targets: []blockTarget{{Scope: "profile", Target: "999"}, {Scope: "profile", Target: itoa(gid)}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.blocksUsing("profile", "999"); len(got) != 1 {
		t.Fatalf("blocksUsing: %v", got)
	}
	a.pruneBlockTargets()
	v := a.blockViews()
	if len(v[0].Targets) != 1 || v[0].Targets[0].Target != itoa(gid) {
		t.Fatalf("the missing profile should be gone: %+v (block %d)", v[0].Targets, id)
	}
	var n int
	a.Config.QueryRow(`SELECT count(*) FROM scope_rules WHERE set_id=?`, id).Scan(&n)
	if n != 1 {
		t.Fatalf("rows should follow: %d", n)
	}
}

