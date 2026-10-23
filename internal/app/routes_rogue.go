package app

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gatehouse/internal/api"
	"gatehouse/internal/dhcp"
)

// Another DHCP server on a network where Gatehouse serves addresses makes devices pick an address at
// random from the two. The check asks the network for offers the way a new device would, takes no
// address, and reports who answered.
func (a *App) registerRogueRoutes() {
	a.API.HandleAdmin("POST /api/dhcp/scopes/{id}/probe", func(w http.ResponseWriter, r *http.Request, c *api.Ctx) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var iface, mode, router string
		if err := a.Config.QueryRow(`SELECT iface,mode,router FROM dhcp_scopes WHERE id=?`, id).Scan(&iface, &mode, &router); err != nil {
			api.Error(w, http.StatusNotFound, "that network does not exist")
			return
		}
		// Our own answers are not a problem: the router address and any address this device has on the port.
		var ours []net.IP
		if ip := net.ParseIP(strings.TrimSpace(router)); ip != nil {
			ours = append(ours, ip)
		}
		if ifi, err := net.InterfaceByName(iface); err == nil {
			addrs, _ := ifi.Addrs()
			for _, ad := range addrs {
				if n, ok := ad.(*net.IPNet); ok {
					ours = append(ours, n.IP)
				}
			}
		}
		ctx, cancel := contextTimeout(6 * time.Second)
		defer cancel()
		found, err := dhcp.Probe(ctx, iface, ours, 4*time.Second)
		if err != nil {
			fail(w, err)
			return
		}
		type finding struct {
			Server string `json:"server"`
			Offer  string `json:"offer"`
		}
		out := []finding{}
		for _, f := range found {
			out = append(out, finding{f.Server.String(), f.Offer.String()})
		}
		verdict := "ok"
		switch {
		case len(out) > 0 && mode == "serve":
			verdict = "conflict" // someone else hands out addresses where Gatehouse does
		case len(out) == 0 && mode == "off":
			verdict = "none" // Gatehouse expects another device to serve here, and nothing answered
		}
		api.JSON(w, 200, map[string]any{"verdict": verdict, "found": out})
	})
}
