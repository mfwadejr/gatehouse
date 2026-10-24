package devices

import "strings"

// Icons are the built-in pictures for a device. Gatehouse guesses one from the maker and the name;
// the person can change it, or upload their own.
var Icons = []string{"computer", "laptop", "phone", "tablet", "tv", "speaker", "printer", "camera", "game", "watch", "iot", "server", "network", "storage", "device"}

// ValidIcon says whether a name is one of the built-in icons.
func ValidIcon(s string) bool {
	for _, i := range Icons {
		if i == s {
			return true
		}
	}
	return false
}

// GuessIcon picks an icon from what is known about a device. A wrong guess is harmless because it
// can be changed.
func GuessIcon(vendor, name, hostname string) string {
	t := strings.ToLower(vendor + " " + name + " " + hostname)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(t, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("iphone", "android", "galaxy", "pixel", "oneplus", "xiaomi", "redmi", "huawei", "oppo", "-phone", "phone"):
		return "phone"
	case has("ipad", "tablet", "kindle", "fire-", "tab-"):
		return "tablet"
	case has("macbook", "laptop", "thinkpad", "latitude", "xps", "inspiron", "surface-", "notebook"):
		return "laptop"
	case has("watch", "fitbit", "garmin"):
		return "watch"
	case has("roku", "chromecast", "bravia", "appletv", "apple-tv", "firetv", "shield", "smarttv", "-tv", "tv-", "vizio", "hisense", "tcl", "lg electronics", "samsung tv"):
		return "tv"
	case has("sonos", "echo", "alexa", "homepod", "speaker", "bose", "google home", "nest-audio", "nest audio"):
		return "speaker"
	case has("printer", "laserjet", "officejet", "canon", "epson", "brother", "lexmark", "xerox", "ricoh", "kyocera"):
		return "printer"
	case has("camera", "cam-", "hikvision", "dahua", "reolink", "amcrest", "wyze", "arlo", "ring", "eufy", "axis comm", "nvr"):
		return "camera"
	case has("playstation", "sony interactive", "xbox", "nintendo", "steam", "valve"):
		return "game"
	case has("synology", "qnap", "nas", "western digital", "drobo"):
		return "storage"
	case has("raspberry", "proxmox", "esxi", "vmware", "server", "docker", "ubuntu", "debian"):
		return "server"
	case has("ubiquiti", "tp-link", "netgear", "cisco", "mikrotik", "aruba", "ruckus", "zyxel", "d-link", "access point", "switch", "router", "gateway"):
		return "network"
	case has("espressif", "tuya", "shelly", "sonoff", "philips lighting", "signify", "hue", "ikea", "tado", "ecobee", "nest", "esp-", "esp32", "esp8266", "wiz", "lifx", "meross", "thermostat", "plug", "bulb"):
		return "iot"
	case has("apple", "dell", "lenovo", "hp inc", "hewlett", "asus", "acer", "msi", "intel", "microsoft", "desktop", "pc-", "-pc", "imac", "mac-"):
		return "computer"
	}
	return "device"
}
