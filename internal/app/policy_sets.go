package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nopewarden/internal/api"
)

// Rule sets and tags. A set groups policies of any kind (a firewall rule, a route, a block, an open
// port, a speed entry) so the whole group can be paused, turned off or turned on together; a policy is
// in at most one set. A tag is a label for finding things: it never changes what anything does. Both
// sit beside the policies in their own tables, so none of the five kinds had to change.

const maxTags = 5

var tagKinds = map[string]bool{"rule": true, "route": true, "block": true, "port": true, "speed": true, "device": true}
var policyKinds = map[string]bool{"rule": true, "route": true, "block": true, "port": true, "speed": true}

var reTagJunk = regexp.MustCompile(`[^a-z0-9 _-]+`)
var reSpaces = regexp.MustCompile(`\s+`)

// cleanTag makes a tag lowercase, plain and at most 24 characters; it is "" when nothing is left.
func cleanTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reTagJunk.ReplaceAllString(s, "")
	s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	if len(s) > 24 {
		s = strings.TrimSpace(s[:24])
	}
	return s
}

// itemRef is how an item is named in the tag and set tables: devices by lower-case MAC address.
func itemRef(kind, ref string) string {
	if kind == "device" {
		return strings.ToLower(ref)
	}
	return ref
}

// dropItem forgets the set and tags of an item that was deleted, so a later item that reuses its
// number does not inherit them.
func (a *App) dropItem(kind, ref string) {
	ref = itemRef(kind, ref)
	a.Config.Exec(`DELETE FROM policy_set_members WHERE kind=? AND ref=?`, kind, ref)
	a.Config.Exec(`DELETE FROM item_tags WHERE kind=? AND ref=?`, kind, ref)
}

type setMember struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type policySet struct {
	ID      int64       `json:"id"`
	Name    string      `json:"name"`
	OffRefs []string    `json:"off_refs"`
	Members []setMember `json:"members"`
}

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func (a *App) setsView() map[string]any {
	sets := []policySet{}
	idx := map[int64]int{}
	if rows, err := a.Config.Query(`SELECT id, name, off_refs FROM policy_sets ORDER BY lower(name)`); err == nil {
		for rows.Next() {
			var s policySet
			var off string
			rows.Scan(&s.ID, &s.Name, &off)
			s.OffRefs = []string{}
			json.Unmarshal([]byte(off), &s.OffRefs)
			s.Members = []setMember{}
			idx[s.ID] = len(sets)
			sets = append(sets, s)
		}
		rows.Close()
	}
	if rows, err := a.Config.Query(`SELECT kind, ref, set_id FROM policy_set_members ORDER BY kind, ref`); err == nil {
		for rows.Next() {
			var m setMember
			var id int64
			rows.Scan(&m.Kind, &m.Ref, &id)
			if i, ok := idx[id]; ok {
				sets[i].Members = append(sets[i].Members, m)
			}
		}
		rows.Close()
	}
	tags := map[string][]string{}
	count := map[string]int{}
	if rows, err := a.Config.Query(`SELECT kind, ref, tag FROM item_tags ORDER BY tag`); err == nil {
		for rows.Next() {
			var k, r, t string
			rows.Scan(&k, &r, &t)
			tags[k+":"+r] = append(tags[k+":"+r], t)
			count[t]++
		}
		rows.Close()
	}
	all := []tagCount{}
	for t, n := range count {
		all = append(all, tagCount{t, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Tag < all[j].Tag })
	return map[string]any{"sets": sets, "tags": tags, "all_tags": all}
}

func cleanSetName(s string) (string, error) {
	s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	if s == "" || len(s) > 40 {
		return "", errors.New("give the set a name of up to 40 characters")
	}
	return s, nil
}

func (a *App) registerSetRoutes() {
	s := a.API
	s.Handle("GET /api/sets", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		api.JSON(w, 200, a.setsView())
	})
	s.HandleAdmin("POST /api/sets", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Name string `json:"name"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		name, err := cleanSetName(in.Name)
		if err != nil {
			fail(w, err)
			return
		}
		var n int
		a.Config.QueryRow(`SELECT COUNT(*) FROM policy_sets WHERE lower(name)=lower(?)`, name).Scan(&n)
		if n > 0 {
			fail(w, errors.New("a set with that name already exists"))
			return
		}
		res, err := a.Config.Exec(`INSERT INTO policy_sets(name, created_at) VALUES(?,?)`, name, time.Now().Unix())
		if err != nil {
			fail(w, err)
			return
		}
		id, _ := res.LastInsertId()
		a.event(c.User.Username, "info", "Rule set \""+name+"\" was made", "")
		api.JSON(w, 200, map[string]any{"id": id})
	})
	s.HandleAdmin("PUT /api/sets/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct {
			Name string `json:"name"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		name, err := cleanSetName(in.Name)
		if err != nil {
			fail(w, err)
			return
		}
		var n int
		a.Config.QueryRow(`SELECT COUNT(*) FROM policy_sets WHERE lower(name)=lower(?) AND id<>?`, name, id).Scan(&n)
		if n > 0 {
			fail(w, errors.New("a set with that name already exists"))
			return
		}
		if res, err := a.Config.Exec(`UPDATE policy_sets SET name=? WHERE id=?`, name, id); err != nil {
			fail(w, err)
			return
		} else if k, _ := res.RowsAffected(); k == 0 {
			fail(w, errors.New("that set does not exist"))
			return
		}
		ok(w)
	})
	// Deleting a set keeps its policies; they just leave the set.
	s.HandleAdmin("DELETE /api/sets/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var name string
		a.Config.QueryRow(`SELECT name FROM policy_sets WHERE id=?`, id).Scan(&name)
		a.Config.Exec(`DELETE FROM policy_set_members WHERE set_id=?`, id)
		a.Config.Exec(`DELETE FROM policy_sets WHERE id=?`, id)
		if name != "" {
			a.event(c.User.Username, "info", "Rule set \""+name+"\" was deleted", "Its policies were kept.")
		}
		ok(w)
	})
	// What was on when the set was turned off, so Turn on brings back exactly those.
	s.HandleAdmin("PUT /api/sets/{id}/memory", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct {
			OffRefs []string `json:"off_refs"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if in.OffRefs == nil {
			in.OffRefs = []string{}
		}
		b, _ := json.Marshal(in.OffRefs)
		a.Config.Exec(`UPDATE policy_sets SET off_refs=? WHERE id=?`, string(b), id)
		ok(w)
	})
	// Put a policy in a set, or take it out (set_id 0).
	s.HandleAdmin("PUT /api/sets/member", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Kind  string `json:"kind"`
			Ref   string `json:"ref"`
			SetID int64  `json:"set_id"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if !policyKinds[in.Kind] || strings.TrimSpace(in.Ref) == "" {
			fail(w, errors.New("sets hold policies only"))
			return
		}
		a.Config.Exec(`DELETE FROM policy_set_members WHERE kind=? AND ref=?`, in.Kind, in.Ref)
		if in.SetID != 0 {
			var n int
			a.Config.QueryRow(`SELECT COUNT(*) FROM policy_sets WHERE id=?`, in.SetID).Scan(&n)
			if n == 0 {
				fail(w, errors.New("that set does not exist"))
				return
			}
			if _, err := a.Config.Exec(`INSERT INTO policy_set_members(kind, ref, set_id) VALUES(?,?,?)`, in.Kind, in.Ref, in.SetID); err != nil {
				fail(w, err)
				return
			}
		}
		ok(w)
	})
	// Replace the tags of one item.
	s.HandleAdmin("PUT /api/tags", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in struct {
			Kind string   `json:"kind"`
			Ref  string   `json:"ref"`
			Tags []string `json:"tags"`
		}
		if !api.Decode(w, r, &in) {
			return
		}
		if !tagKinds[in.Kind] || strings.TrimSpace(in.Ref) == "" {
			fail(w, errors.New("unknown item"))
			return
		}
		seen := map[string]bool{}
		var tags []string
		for _, t := range in.Tags {
			if t = cleanTag(t); t != "" && !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
		if len(tags) > maxTags {
			fail(w, errors.New("an item can have "+strconv.Itoa(maxTags)+" tags; remove one to add another"))
			return
		}
		ref := itemRef(in.Kind, in.Ref)
		tx, err := a.Config.Begin()
		if err != nil {
			fail(w, err)
			return
		}
		defer tx.Rollback()
		tx.Exec(`DELETE FROM item_tags WHERE kind=? AND ref=?`, in.Kind, ref)
		for _, t := range tags {
			if _, err := tx.Exec(`INSERT INTO item_tags(kind, ref, tag) VALUES(?,?,?)`, in.Kind, ref, t); err != nil {
				fail(w, err)
				return
			}
		}
		if err := tx.Commit(); err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]any{"tags": tags})
	})
}
