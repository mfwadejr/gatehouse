package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// policyChange puts every change to a rule-like item (firewall rules, routes, blocks, port forwards)
// in Admin activity, in plain words: created or edited, turned on or off, paused, resumed, removed.
func (a *App) policyChange(actor, method, path string, body []byte) {
	a.stagedAfter(actor, method, path, body)
	if strings.HasPrefix(path, "/api/routes") {
		forgetRouteNames()
	}
	what, ok := describePolicyChange(method, path, body)
	if !ok {
		return
	}
	a.auditEvent(actor, "info", what[0], what[1])
}

func describePolicyChange(method, path string, body []byte) ([2]string, bool) {
	var in struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
		Group   string `json:"group"`
		Minutes *int   `json:"minutes"`
		Sched   *struct {
			Mode string `json:"mode"`
		} `json:"sched"`
	}
	json.Unmarshal(body, &in)
	kinds := []struct{ prefix, noun string }{
		{"/api/fw/rules", "firewall rule"}, {"/api/routes", "route"}, {"/api/blocks", "block"},
		{"/api/fw/appforwards", "port forward"}, {"/api/fw/forwards", "port forward"},
	}
	noun := ""
	for _, k := range kinds {
		if path == k.prefix || strings.HasPrefix(path, k.prefix+"/") {
			noun = k.noun
			break
		}
	}
	if noun == "" {
		switch path {
		case "/api/fw/apply":
			return [2]string{"Firewall changes applied", "The pending firewall changes were applied."}, true
		case "/api/fw/rollback":
			return [2]string{"Firewall changes rolled back", "The last firewall change was rolled back."}, true
		}
		return [2]string{}, false
	}
	name := in.Name
	if name == "" {
		name = in.Group
	}
	label := noun
	if name != "" {
		label = fmt.Sprintf("%s “%s”", noun, name)
	} else if in.ID != 0 {
		label = fmt.Sprintf("%s #%d", noun, in.ID)
	}
	up := strings.ToUpper(noun[:1]) + noun[1:]
	switch {
	case strings.HasSuffix(path, "/pause"):
		if in.Minutes != nil && *in.Minutes == 0 {
			return [2]string{up + " resumed", "Resumed " + label + "."}, true
		}
		if in.Minutes != nil && *in.Minutes > 0 {
			return [2]string{up + " paused", fmt.Sprintf("Paused %s for %d minutes.", label, *in.Minutes)}, true
		}
		return [2]string{up + " paused", "Paused " + label + " until resumed."}, true
	case strings.HasSuffix(path, "/enable"):
		if in.Enabled != nil && !*in.Enabled {
			return [2]string{up + " turned off", "Turned off " + label + "."}, true
		}
		return [2]string{up + " turned on", "Turned on " + label + "."}, true
	case strings.HasSuffix(path, "/order"):
		return [2]string{up + " order changed", "Changed the order of the " + noun + "s."}, true
	case method == "DELETE":
		return [2]string{up + " removed", "Removed " + label + "."}, true
	case method == "PUT":
		verb := "Saved"
		if in.ID == 0 {
			verb = "Created"
		}
		extra := ""
		if in.Sched != nil && in.Sched.Mode != "" && in.Sched.Mode != "always" {
			extra = " It runs on a schedule."
		}
		return [2]string{up + " " + strings.ToLower(verb), verb + " " + label + "." + extra}, true
	}
	return [2]string{}, false
}
