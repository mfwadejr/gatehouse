package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"gatehouse/internal/api"
	dnssrv "gatehouse/internal/dns"
	"gatehouse/internal/fw"
)

// Profiles are the friendly face of the DNS groups: a name for a kind of person or device (Kids,
// Guests, Smart home, Work) with a filtering level, optional safe search, optional off-hours and a
// pause button. Underneath they are the same groups the DNS page already shows.

type category struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Descr  string `json:"descr"`
	URL    string `json:"url"`
	Format string `json:"format"`
}

// The addresses of these lists could not be fetched from the build machine, so they are marked as
// unverified in the console until the first download works.
var categoryCatalog = []category{
	{"ads", "Ads and trackers", "Advertising, tracking and annoying pop-ups.", "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt", "abp"},
	{"malware", "Malware and phishing", "Sites known to spread viruses or steal passwords.", "https://urlhaus.abuse.ch/downloads/hostfile/", "hosts"},
	{"adult", "Adult content", "Pornography.", "https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/porn-only/hosts", "hosts"},
	{"gambling", "Gambling", "Betting and casino sites.", "https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/gambling-only/hosts", "hosts"},
	{"social", "Social media", "Facebook, Instagram, TikTok and the like.", "https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/social-only/hosts", "hosts"},
}

type presetDef struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Descr      string   `json:"descr"`
	Level      string   `json:"level"`
	Categories []string `json:"categories"`
	SafeSearch bool     `json:"safe_search"`
	Hours      fw.Hours `json:"hours"`
}

var presetCatalog = []presetDef{
	{"kids", "Kids", "Strong filtering, safe search, and no internet from 9 pm to 7 am.", "enhanced", nil, true, fw.Hours{On: true, Days: []int{0, 1, 2, 3, 4, 5, 6}, From: "21:00", To: "07:00"}},
	{"guests", "Guests", "Everyday protection from ads and malware.", "basic", nil, false, fw.Hours{}},
	{"smart", "Smart home", "Only blocks known malware, so gadgets keep working.", "custom", []string{"malware"}, false, fw.Hours{}},
	{"work", "Work", "Everyday protection from ads and malware.", "basic", nil, false, fw.Hours{}},
}

func levelCategories(level string, custom []string) []string {
	switch level {
	case "basic":
		return []string{"ads", "malware"}
	case "enhanced":
		return []string{"ads", "malware", "adult", "gambling"}
	case "custom":
		var out []string
		for _, c := range categoryCatalog {
			for _, k := range custom {
				if k == c.Key {
					out = append(out, k)
				}
			}
		}
		return out
	}
	return nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func levelOf(filtering bool, cats []string) string {
	switch {
	case !filtering:
		return "off"
	case sameSet(cats, []string{"ads", "malware"}):
		return "basic"
	case sameSet(cats, []string{"ads", "malware", "adult", "gambling"}):
		return "enhanced"
	}
	return "custom"
}

// categoryListID finds the blocklist behind a category, or creates it when it is not there yet.
func (a *App) categoryListID(c category) (int64, error) {
	var id int64
	err := a.Config.QueryRow(`SELECT id FROM blocklists WHERE url=?`, c.URL).Scan(&id)
	if err == nil {
		return id, nil
	}
	id, err = a.Lists.Add("Category: "+c.Name, c.URL, c.Format, 24)
	if err != nil {
		return 0, err
	}
	// Added lists join the Default group; a category belongs only to the profiles that choose it.
	a.Config.Exec(`DELETE FROM group_blocklists WHERE blocklist_id=? AND group_id IN (SELECT id FROM groups WHERE name='Default')`, id)
	a.Lists.Nudge()
	return id, nil
}

func (a *App) categoryIDs() map[string]int64 {
	out := map[string]int64{}
	for _, c := range categoryCatalog {
		var id int64
		if a.Config.QueryRow(`SELECT id FROM blocklists WHERE url=?`, c.URL).Scan(&id) == nil {
			out[c.Key] = id
		}
	}
	return out
}

type profileOut struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Preset      string   `json:"preset"`
	Level       string   `json:"level"`
	Categories  []string `json:"categories"`
	SafeSearch  bool     `json:"safe_search"`
	Hours       fw.Hours `json:"hours"`
	PausedUntil int64    `json:"paused_until"`
	Devices     int      `json:"devices"`
	OffNow      bool     `json:"off_now"`
}

func (a *App) profileList() ([]profileOut, error) {
	groups, err := a.Lists.Groups()
	if err != nil {
		return nil, err
	}
	ids := a.categoryIDs()
	extra := map[int64]struct {
		preset, hours string
		safe          bool
		paused        int64
	}{}
	rows, err := a.Config.Query(`SELECT id,preset,hours,safe_search,paused_until FROM groups`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, p int64
		var preset, hours string
		var s int
		if rows.Scan(&id, &preset, &hours, &s, &p) == nil {
			extra[id] = struct {
				preset, hours string
				safe          bool
				paused        int64
			}{preset, hours, s == 1, p}
		}
	}
	rows.Close()
	now := time.Now()
	var out []profileOut
	for _, g := range groups {
		x := extra[g.ID]
		cats := []string{}
		for _, c := range categoryCatalog {
			if id, ok := ids[c.Key]; ok {
				for _, l := range g.Lists {
					if l == id {
						cats = append(cats, c.Key)
					}
				}
			}
		}
		h := fw.ParseHours(x.hours)
		if h.Days == nil {
			h.Days = []int{}
		}
		out = append(out, profileOut{ID: g.ID, Name: g.Name, Description: g.Description, Preset: x.preset,
			Level: levelOf(g.Filtering, cats), Categories: cats, SafeSearch: x.safe, Hours: h, PausedUntil: x.paused,
			Devices: g.Devices, OffNow: x.paused > now.Unix() || h.Active(now)})
	}
	return out, nil
}

type profileIn struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Preset      string   `json:"preset"`
	Level       string   `json:"level"`
	Categories  []string `json:"categories"`
	SafeSearch  bool     `json:"safe_search"`
	Hours       fw.Hours `json:"hours"`
}

func (a *App) saveProfile(in profileIn) (int64, error) {
	if !in.Hours.Valid() {
		return 0, errors.New("the off-hours need at least one day and two different times, like 21:00 and 07:00")
	}
	if strings.ContainsAny(strings.TrimSpace(in.Name), " ,:") {
		// Group names are used in firewall rules, where spaces and commas separate things.
		in.Name = strings.NewReplacer(" ", "-", ",", "-", ":", "-").Replace(strings.TrimSpace(in.Name))
	}
	switch in.Level {
	case "off", "basic", "enhanced", "custom":
	default:
		return 0, errors.New("choose Off, Basic, Enhanced or Custom")
	}
	// Keep the lists somebody added by hand; replace only the category lists.
	catIDs := a.categoryIDs()
	isCat := map[int64]bool{}
	for _, id := range catIDs {
		isCat[id] = true
	}
	var lists []int64
	if in.ID > 0 {
		groups, err := a.Lists.Groups()
		if err != nil {
			return 0, err
		}
		for _, g := range groups {
			if g.ID == in.ID {
				for _, l := range g.Lists {
					if !isCat[l] {
						lists = append(lists, l)
					}
				}
			}
		}
	}
	if in.Level != "off" {
		for _, k := range levelCategories(in.Level, in.Categories) {
			for _, c := range categoryCatalog {
				if c.Key == k {
					id, err := a.categoryListID(c)
					if err != nil {
						return 0, err
					}
					lists = append(lists, id)
				}
			}
		}
	}
	if lists == nil {
		lists = []int64{}
	}
	id, err := a.Lists.SaveGroup(dnssrv.Group{ID: in.ID, Name: in.Name, Description: in.Description, Filtering: in.Level != "off", Lists: lists})
	if err != nil {
		return 0, err
	}
	hb, _ := json.Marshal(in.Hours)
	hours := string(hb)
	if !in.Hours.On {
		hours = ""
	}
	if _, err := a.Config.Exec(`UPDATE groups SET preset=?,safe_search=?,hours=? WHERE id=?`, in.Preset, b2i(in.SafeSearch), hours, id); err != nil {
		return 0, err
	}
	a.Devices.Rebuild()
	a.fwSync()
	return id, nil
}

// pauseUntil turns minutes into a time: 0 resumes, a negative number pauses until resumed.
func pauseUntil(minutes int, now time.Time) int64 {
	switch {
	case minutes == 0:
		return 0
	case minutes < 0 || minutes > 60*24*365:
		return now.Add(10 * 365 * 24 * time.Hour).Unix()
	}
	return now.Add(time.Duration(minutes) * time.Minute).Unix()
}

// pausedSig is the set of paused devices as one string, so the background loop can tell when an
// expiry or a profile's hours changed it.
func (a *App) pausedSig() string {
	m := fw.PausedDevices(a.Config, time.Now())
	sort.Strings(m)
	return strings.Join(m, ",")
}

func (a *App) registerProfileRoutes() {
	s := a.API
	s.Handle("GET /api/profiles", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		list, err := a.profileList()
		if err != nil {
			fail(w, err)
			return
		}
		ids := a.categoryIDs()
		type cat struct {
			category
			Ready bool `json:"ready"`
		}
		var cats []cat
		for _, c := range categoryCatalog {
			_, ok := ids[c.Key]
			cats = append(cats, cat{c, ok})
		}
		api.JSON(w, 200, map[string]any{"profiles": list, "presets": presetCatalog, "categories": cats})
	})
	s.HandleAdmin("PUT /api/profiles", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		var in profileIn
		if !api.Decode(w, r, &in) {
			return
		}
		id, err := a.saveProfile(in)
		if err != nil {
			fail(w, err)
			return
		}
		api.JSON(w, 200, map[string]int64{"id": id})
	})
	s.HandleAdmin("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, good := pathID(r)
		if !good {
			fail(w, errBadID)
			return
		}
		if err := a.Lists.DeleteGroup(id); err != nil {
			fail(w, err)
			return
		}
		a.Devices.Rebuild()
		a.fwSync()
		ok(w)
	})
	pause := func(table string) func(http.ResponseWriter, *http.Request, *api.Ctx) {
		return func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
			id, good := pathID(r)
			if !good {
				fail(w, errBadID)
				return
			}
			var in struct {
				Minutes int `json:"minutes"`
			}
			if !api.Decode(w, r, &in) {
				return
			}
			res, err := a.Config.Exec(`UPDATE `+table+` SET paused_until=? WHERE id=?`, pauseUntil(in.Minutes, time.Now()), id)
			if err != nil {
				fail(w, err)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				fail(w, errBadID)
				return
			}
			a.dyn.lastPaused.Store(a.pausedSig())
			a.fwSync()
			ok(w)
		}
	}
	s.HandleAdmin("POST /api/profiles/{id}/pause", pause("groups"))
	s.HandleAdmin("POST /api/devices/{id}/pause", pause("devices"))
}
