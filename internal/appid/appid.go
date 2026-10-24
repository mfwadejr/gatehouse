// Package appid names the application behind a website name, such as "YouTube" for
// googlevideo.com. It works from names a device asked for through Gatehouse's DNS, so it needs
// no packet inspection. It only names what it is sure of; everything else stays unnamed.
package appid

import "strings"

// App is one named application.
type App struct {
	Name     string
	Category string
}

type entry struct {
	app     string
	cat     string
	domains string // space separated suffixes
}

var catalogue = []entry{
	{"YouTube", "Video", "youtube.com youtu.be googlevideo.com ytimg.com youtube-nocookie.com"},
	{"Netflix", "Video", "netflix.com nflxvideo.net nflximg.net nflxext.com nflxso.net"},
	{"Disney+", "Video", "disneyplus.com disney-plus.net dssott.com bamgrid.com"},
	{"Prime Video", "Video", "primevideo.com aiv-cdn.net aiv-delivery.net"},
	{"Hulu", "Video", "hulu.com huluim.com hulustream.com"},
	{"Max", "Video", "max.com hbomax.com hbo.com"},
	{"Twitch", "Video", "twitch.tv ttvnw.net jtvnw.net twitchcdn.net"},
	{"Plex", "Video", "plex.tv plex.direct"},
	{"Apple TV+", "Video", "tv.apple.com"},
	{"Vimeo", "Video", "vimeo.com vimeocdn.com"},
	{"Spotify", "Music", "spotify.com scdn.co spotifycdn.com"},
	{"Apple Music", "Music", "music.apple.com applemusic.com"},
	{"Pandora", "Music", "pandora.com"},
	{"SoundCloud", "Music", "soundcloud.com sndcdn.com"},
	{"Facebook", "Social", "facebook.com fbcdn.net fb.com facebook.net"},
	{"Instagram", "Social", "instagram.com cdninstagram.com"},
	{"WhatsApp", "Messaging", "whatsapp.com whatsapp.net"},
	{"TikTok", "Social", "tiktok.com tiktokcdn.com tiktokv.com byteoversea.com musical.ly"},
	{"X (Twitter)", "Social", "twitter.com x.com twimg.com t.co"},
	{"Reddit", "Social", "reddit.com redd.it redditmedia.com redditstatic.com"},
	{"Snapchat", "Social", "snapchat.com sc-cdn.net snap.com"},
	{"Pinterest", "Social", "pinterest.com pinimg.com"},
	{"LinkedIn", "Social", "linkedin.com licdn.com"},
	{"Discord", "Messaging", "discord.com discord.gg discordapp.com discordapp.net"},
	{"Telegram", "Messaging", "telegram.org t.me telegram.me"},
	{"Signal", "Messaging", "signal.org whispersystems.org"},
	{"Zoom", "Calls", "zoom.us zoom.com zoomgov.com"},
	{"Microsoft Teams", "Calls", "teams.microsoft.com teams.live.com"},
	{"Webex", "Calls", "webex.com wbx2.com"},
	{"Skype", "Calls", "skype.com"},
	{"Google Meet", "Calls", "meet.google.com"},
	{"FaceTime", "Calls", "facetime.apple.com"},
	{"Google", "Search", "google.com gstatic.com googleapis.com gvt1.com googleusercontent.com ggpht.com"},
	{"Gmail", "Email", "gmail.com mail.google.com"},
	{"Microsoft 365", "Work", "office.com office365.com microsoftonline.com live.com outlook.com sharepoint.com onedrive.com microsoft.com msftconnecttest.com"},
	{"Apple", "System", "apple.com icloud.com icloud-content.com mzstatic.com cdn-apple.com apple-dns.net"},
	{"Windows Update", "Updates", "windowsupdate.com delivery.mp.microsoft.com update.microsoft.com"},
	{"Google Drive", "Storage", "drive.google.com docs.google.com"},
	{"Dropbox", "Storage", "dropbox.com dropboxstatic.com dropboxusercontent.com"},
	{"Amazon", "Shopping", "amazon.com amazon.co.uk amazon.de media-amazon.com ssl-images-amazon.com"},
	{"AWS", "Cloud", "amazonaws.com cloudfront.net awsstatic.com"},
	{"eBay", "Shopping", "ebay.com ebaystatic.com ebayimg.com"},
	{"Steam", "Games", "steampowered.com steamcontent.com steamstatic.com steamcommunity.com steamserver.net"},
	{"Epic Games", "Games", "epicgames.com unrealengine.com epicgames.dev"},
	{"Xbox", "Games", "xboxlive.com xbox.com"},
	{"PlayStation", "Games", "playstation.com playstation.net sonyentertainmentnetwork.com"},
	{"Nintendo", "Games", "nintendo.com nintendo.net"},
	{"Roblox", "Games", "roblox.com rbxcdn.com"},
	{"Fortnite", "Games", "fortnite.com"},
	{"Minecraft", "Games", "minecraft.net mojang.com"},
	{"Blizzard", "Games", "blizzard.com battle.net"},
	{"GitHub", "Work", "github.com githubusercontent.com githubassets.com"},
	{"Slack", "Work", "slack.com slack-edge.com slack-msgs.com"},
	{"Notion", "Work", "notion.so notion.com"},
	{"Cloudflare", "Cloud", "cloudflare.com cloudflare-dns.com cloudflarestream.com"},
	{"Akamai", "Cloud", "akamaihd.net akamaized.net akamai.net edgekey.net"},
	{"Fastly", "Cloud", "fastly.net fastlylb.net"},
	{"Ring", "Home devices", "ring.com"},
	{"Nest", "Home devices", "nest.com nestlabs.com"},
	{"Philips Hue", "Home devices", "meethue.com"},
	{"Sonos", "Home devices", "sonos.com"},
	{"Roku", "Video", "roku.com"},
	{"Samsung", "System", "samsung.com samsungcloudsolution.com samsungapps.com samsungosp.com"},
	{"Tailscale", "VPN", "tailscale.com tailscale.io"},
	{"Mullvad", "VPN", "mullvad.net"},
	{"NordVPN", "VPN", "nordvpn.com nordvpn.net"},
	{"Proton", "VPN", "proton.me protonvpn.com protonmail.com"},
	{"Time sync", "System", "ntp.org pool.ntp.org time.windows.com time.apple.com"},
	{"ChatGPT", "AI", "openai.com chatgpt.com oaiusercontent.com"},
	{"Claude", "AI", "anthropic.com claude.ai"},
	{"Ads and tracking", "Advertising", "doubleclick.net googlesyndication.com googleadservices.com adservice.google.com scorecardresearch.com facebook-analytics.com adnxs.com"},
}

var index map[string]App

func init() {
	index = map[string]App{}
	for _, e := range catalogue {
		for _, d := range strings.Fields(e.domains) {
			index[d] = App{e.app, e.cat}
		}
	}
}

// Lookup names the application behind a website name. It tries the whole name, then each
// shorter ending, so "r4---sn-abc.googlevideo.com" finds googlevideo.com.
func Lookup(name string) (App, bool) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	for name != "" {
		if a, ok := index[name]; ok {
			return a, true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[i+1:]
	}
	return App{}, false
}

// Domains lists the endings that belong to an application, for searching the log by app.
func Domains(app string) []string {
	var out []string
	for _, e := range catalogue {
		if strings.EqualFold(e.app, app) {
			out = append(out, strings.Fields(e.domains)...)
		}
	}
	return out
}

// Names lists every application the catalogue knows.
func Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range catalogue {
		if !seen[e.app] {
			seen[e.app] = true
			out = append(out, e.app)
		}
	}
	return out
}
