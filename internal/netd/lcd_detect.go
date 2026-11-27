package netd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding the display. Nothing here changes anything: it reads what the machine says about itself,
// looks at USB and serial ports and the I2C buses, and asks serial ports the one question that
// Crystalfontz panels answer. Everything found is reported, so even a display that is not recognised
// shows the facts someone needs to set it up by hand.

type LCDUSB struct {
	ID      string `json:"id"` // vendor:product
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Note    string `json:"note"`
}

type LCDProbe struct {
	Model    string `json:"model"` // CFA631
	Firmware string `json:"firmware"`
	Speed    int    `json:"speed"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Keys     int    `json:"keys"`
}

type LCDSerial struct {
	Path  string    `json:"path"`
	UART  string    `json:"uart"`
	Note  string    `json:"note"`
	Probe *LCDProbe `json:"probe,omitempty"`
}

type LCDI2C struct {
	Bus   string   `json:"bus"` // /dev/i2c-0
	Name  string   `json:"name"`
	Found []string `json:"found"` // addresses that answered in the range display adapters use
	Note  string   `json:"note"`
}

// LCDMatch is a way to run the display that the facts point to. Sure says how far to trust it:
// confirmed (the display answered), usb (its USB name), guess (only the machine's make or a chip address).
type LCDMatch struct {
	Driver   string            `json:"driver"`
	Settings map[string]string `json:"settings"`
	Sure     string            `json:"sure"`
	Why      string            `json:"why"`
	Keys     int               `json:"keys"`
}

type LCDFacts struct {
	Vendor   string      `json:"vendor"`
	Product  string      `json:"product"`
	Board    string      `json:"board"`
	USB      []LCDUSB    `json:"usb"`
	Serial   []LCDSerial `json:"serial"`
	I2C      []LCDI2C    `json:"i2c"`
	Parallel []string    `json:"parallel"`
	Matches  []LCDMatch  `json:"matches"`
	Notes    []string    `json:"notes"`
	Report   string      `json:"report"` // everything above as plain text, to copy
}

// cfaCRC is the checksum Crystalfontz packets carry: CRC-16, polynomial 0x8408, start 0xFFFF, inverted.
func cfaCRC(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, x := range b {
		crc ^= uint16(x)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// cfaPacket builds one command packet: type, length, data, checksum (low byte first).
func cfaPacket(typ byte, data []byte) []byte {
	p := append([]byte{typ, byte(len(data))}, data...)
	c := cfaCRC(p)
	return append(p, byte(c), byte(c>>8))
}

var reCFAVersion = regexp.MustCompile(`(?i)(CFA-?\s?\d{3})[A-Za-z0-9-]*\s*:?\s*h?([\w.]*)\s*,?\s*v?([\w.]*)`)

// cfaModels says what each panel is: its text size and how many keys it has.
var cfaModels = map[string]LCDProbe{
	"CFA533": {Width: 16, Height: 2, Keys: 6},
	"CFA631": {Width: 20, Height: 2, Keys: 6},
	"CFA633": {Width: 16, Height: 2, Keys: 6},
	"CFA635": {Width: 20, Height: 4, Keys: 6},
}

// parseCFAReply finds a version answer (type 0x41) in what a serial port returned.
func parseCFAReply(b []byte) (LCDProbe, bool) {
	for i := 0; i+2 < len(b); i++ {
		if b[i] != 0x41 {
			continue
		}
		n := int(b[i+1])
		if n < 4 || i+2+n+2 > len(b) {
			continue
		}
		data := b[i+2 : i+2+n]
		crc := uint16(b[i+2+n]) | uint16(b[i+3+n])<<8
		if cfaCRC(b[i:i+2+n]) != crc {
			continue
		}
		m := reCFAVersion.FindStringSubmatch(string(data))
		if m == nil {
			continue
		}
		model := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(m[1]))
		p := cfaModels[model]
		p.Model, p.Firmware = model, strings.Trim(strings.TrimSpace("hardware "+m[2]+", firmware "+m[3]), ", ")
		return p, true
	}
	return LCDProbe{}, false
}

// lcdProbeSerial asks a serial port who is there. A variable so tests can answer for it.
var lcdProbeSerial = func(dev string) *LCDProbe {
	for _, speed := range []int{19200, 115200} {
		if _, err := sysRun(5*time.Second, "stty", "-F", dev, "raw", "-echo", "-hupcl", "cs8", "-cstopb", "-parenb", "-crtscts", strconv.Itoa(speed)); err != nil {
			return nil
		}
		f, err := os.OpenFile(dev, os.O_RDWR|syscallNoctty|syscallNonblock, 0)
		if err != nil {
			return nil
		}
		f.Write(cfaPacket(0x01, nil))
		var got []byte
		buf := make([]byte, 128)
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			f.SetReadDeadline(deadline)
			n, err := f.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil || len(got) > 200 {
				break
			}
			if _, ok := parseCFAReply(got); ok {
				break
			}
		}
		f.Close()
		if p, ok := parseCFAReply(got); ok {
			p.Speed = speed
			return &p
		}
	}
	return nil
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

// usbDisplayNote recognises display makers by the names a USB device gives itself.
func usbDisplayNote(vendor, product, id string) (note, driver string) {
	l := strings.ToLower(vendor + " " + product)
	switch {
	case strings.Contains(l, "crystalfontz") || strings.Contains(l, "cfa-") || strings.Contains(l, "cfa6") || strings.Contains(l, "cfa5"):
		return "A Crystalfontz display.", "CFontz633"
	case strings.Contains(l, "matrix orbital"):
		return "A Matrix Orbital display.", "mtxorb"
	case strings.Contains(l, "pertelian"):
		return "A Pertelian display.", "hd44780"
	case strings.Contains(l, "iowarrior") || strings.HasPrefix(id, "07c0:"):
		return "An IO-Warrior, which drives some displays.", "IOWarrior"
	case id == "0403:6001" || id == "0403:6015" || id == "10c4:ea60" || id == "067b:2303" || strings.Contains(l, "ftdi") || strings.Contains(l, "uart") || strings.Contains(l, "serial"):
		return "A USB serial adapter. A display, or a UPS or modem, may be behind it; the serial check below tells.", ""
	}
	return "", ""
}

// applianceNote names makes that often have a built-in front panel.
func applianceNote(vendor, product string) string {
	l := strings.ToLower(vendor + " " + product)
	for _, w := range []string{"sophos", "watchguard", "lanner", "advantech", "axiomtek", "netgate", "protectli", "qotom", "supermicro", "fortinet", "cisco", "dell", "hp", "ibm", "thomas-krenn", "iei", "portwell", "kontron", "nexcom", "seco"} {
		if strings.Contains(l, w) {
			return "Makers like " + w + " fit some models with a front panel display."
		}
	}
	return ""
}

var reI2CAddr = regexp.MustCompile(`\b([0-9a-f]{2})\b`)

// parseI2CDetect reads i2cdetect's table and returns the addresses that answered.
func parseI2CDetect(out string) []string {
	var found []string
	for _, ln := range strings.Split(out, "\n") {
		if i := strings.Index(ln, ":"); i > 0 && len(strings.TrimSpace(ln[:i])) == 2 {
			row := strings.TrimSpace(ln[:i])
			for _, m := range reI2CAddr.FindAllString(ln[i+1:], -1) {
				if m != "--" && m != "UU" {
					found = append(found, "0x"+m)
				}
			}
			_ = row
		}
	}
	return found
}

// LCDDetect gathers the facts about the machine and any display on it.
func (s *Service) LCDDetect() (LCDFacts, error) {
	var f LCDFacts
	dmi := func(n string) string { return readTrim(s.rootPath("/sys/class/dmi/id/" + n)) }
	f.Vendor, f.Product, f.Board = dmi("sys_vendor"), dmi("product_name"), dmi("board_name")
	if n := applianceNote(f.Vendor, f.Product+" "+dmi("board_vendor")); n != "" {
		f.Notes = append(f.Notes, n)
	}

	// USB devices.
	devs, _ := filepath.Glob(s.rootPath("/sys/bus/usb/devices/*"))
	for _, d := range devs {
		vid, pid := readTrim(d+"/idVendor"), readTrim(d+"/idProduct")
		if vid == "" || pid == "" || strings.HasPrefix(filepath.Base(d), "usb") {
			continue
		}
		u := LCDUSB{ID: vid + ":" + pid, Vendor: readTrim(d + "/manufacturer"), Product: readTrim(d + "/product")}
		note, drv := usbDisplayNote(u.Vendor, u.Product, u.ID)
		u.Note = note
		if drv != "" {
			set := map[string]string{}
			switch drv {
			case "CFontz633":
				set = map[string]string{"Model": "633", "Speed": "19200"}
			case "mtxorb":
				set = map[string]string{"Speed": "19200"}
			}
			f.Matches = append(f.Matches, LCDMatch{Driver: drv, Settings: set, Sure: "usb", Why: fmt.Sprintf("USB device %s (%s %s) looks like a display.", u.ID, u.Vendor, u.Product)})
		}
		f.USB = append(f.USB, u)
	}

	// Serial ports: real ones, plus USB serial adapters. A port someone else has open is left alone.
	var ports []string
	for _, pat := range []string{"/dev/ttyS*", "/dev/ttyUSB*", "/dev/ttyACM*"} {
		m, _ := filepath.Glob(s.rootPath(pat))
		ports = append(ports, m...)
	}
	sort.Strings(ports)
	cmdline := readTrim(s.rootPath("/proc/cmdline"))
	for _, p := range ports {
		name := filepath.Base(p)
		sp := LCDSerial{Path: "/dev/" + name}
		if strings.HasPrefix(name, "ttyS") {
			if _, err := os.Stat(s.rootPath("/sys/class/tty/" + name + "/device")); err != nil {
				continue // no real port behind it
			}
			if b, err := os.ReadFile(s.rootPath("/proc/tty/driver/serial")); err == nil {
				n := strings.TrimPrefix(name, "ttyS")
				for _, ln := range strings.Split(string(b), "\n") {
					if strings.HasPrefix(ln, n+":") {
						if i := strings.Index(ln, "uart:"); i >= 0 {
							sp.UART = strings.Fields(ln[i+5:])[0]
						}
					}
				}
			}
			if sp.UART == "unknown" {
				continue
			}
		}
		switch {
		case strings.Contains(cmdline, "console="+name):
			sp.Note = "Used as this machine's serial console, so it was not asked."
		case lcdPortInUse(s, sp.Path):
			sp.Note = "Another program is using this port (a UPS, for example), so it was not asked."
		default:
			sp.Probe = lcdProbeSerial(sp.Path)
			if sp.Probe == nil {
				sp.Note = "Nothing answered the Crystalfontz question. That does not rule out another kind of display."
			} else {
				pr := sp.Probe
				f.Matches = append(f.Matches, LCDMatch{
					Driver: "CFontz633", Sure: "confirmed", Keys: pr.Keys,
					Settings: map[string]string{"Device": sp.Path, "Model": strings.TrimPrefix(pr.Model, "CFA"), "Speed": strconv.Itoa(pr.Speed), "Size": fmt.Sprintf("%dx%d", pr.Width, pr.Height)},
					Why:      fmt.Sprintf("The display on %s answered: %s (%s), %dx%d characters, %d keys.", sp.Path, pr.Model, pr.Firmware, pr.Width, pr.Height, pr.Keys),
				})
			}
		}
		f.Serial = append(f.Serial, sp)
	}

	// I2C buses, where the common "backpack" display adapters sit.
	buses, _ := filepath.Glob(s.rootPath("/dev/i2c-*"))
	sort.Strings(buses)
	for _, b := range buses {
		n := filepath.Base(b)
		bus := LCDI2C{Bus: "/dev/" + n, Name: readTrim(s.rootPath("/sys/class/i2c-dev/" + n + "/name"))}
		if _, err := lookPath("i2cdetect"); err != nil {
			bus.Note = "The i2c-tools package is not installed, so this bus was not scanned."
		} else if out, err := sysRun(20*time.Second, "i2cdetect", "-y", "-r", strings.TrimPrefix(n, "i2c-"), "0x20", "0x3f"); err == nil {
			bus.Found = parseI2CDetect(out)
			for _, a := range bus.Found {
				v, _ := strconv.ParseInt(strings.TrimPrefix(a, "0x"), 16, 32)
				if (v >= 0x20 && v <= 0x27) || (v >= 0x38 && v <= 0x3f) {
					f.Matches = append(f.Matches, LCDMatch{
						Driver: "hd44780", Sure: "guess",
						Settings: map[string]string{"ConnectionType": "i2c", "Device": bus.Bus, "Port": a, "Size": "20x2"},
						Why:      fmt.Sprintf("Something answered at address %s on %s, where display adapters usually are. It cannot say what it is, so the size is a guess.", a, bus.Bus),
					})
				}
			}
		}
		f.I2C = append(f.I2C, bus)
	}

	// Parallel ports, which old display boards use. They cannot be asked anything.
	pp, _ := filepath.Glob(s.rootPath("/dev/parport*"))
	for _, p := range pp {
		f.Parallel = append(f.Parallel, "/dev/"+filepath.Base(p))
	}
	if b, err := os.ReadFile(s.rootPath("/proc/ioports")); err == nil && strings.Contains(string(b), "parport") {
		f.Notes = append(f.Notes, "This machine has a parallel port. Older display boards use one, and nothing can be asked over it.")
	}
	sort.SliceStable(f.Matches, func(i, j int) bool { return lcdSureRank(f.Matches[i].Sure) < lcdSureRank(f.Matches[j].Sure) })
	if len(f.Matches) == 0 {
		f.Notes = append(f.Notes, "No display was recognised. The facts above are what this machine reports; with the make and model of the box they show which driver to choose.")
	}
	f.Report = lcdReport(f)
	return f, nil
}

func lcdSureRank(s string) int {
	switch s {
	case "confirmed":
		return 0
	case "usb":
		return 1
	}
	return 2
}

// lcdPortInUse says whether a process has the device open, by looking at the open files of every process.
func lcdPortInUse(s *Service, dev string) bool {
	fds, _ := filepath.Glob(s.rootPath("/proc/[0-9]*/fd/*"))
	for _, fd := range fds {
		if t, err := os.Readlink(fd); err == nil && t == dev {
			return true
		}
	}
	return false
}

// lcdReport writes the facts as plain text, for pasting into a message.
func lcdReport(f LCDFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Machine: %s %s (board %s)\n", f.Vendor, f.Product, f.Board)
	fmt.Fprintf(&b, "USB devices: %d\n", len(f.USB))
	for _, u := range f.USB {
		fmt.Fprintf(&b, "  %s  %s %s  %s\n", u.ID, u.Vendor, u.Product, u.Note)
	}
	fmt.Fprintf(&b, "Serial ports: %d\n", len(f.Serial))
	for _, p := range f.Serial {
		fmt.Fprintf(&b, "  %s  uart=%s  %s", p.Path, p.UART, p.Note)
		if p.Probe != nil {
			fmt.Fprintf(&b, "answered: %s %s at %d baud, %dx%d", p.Probe.Model, p.Probe.Firmware, p.Probe.Speed, p.Probe.Width, p.Probe.Height)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "I2C buses: %d\n", len(f.I2C))
	for _, i := range f.I2C {
		fmt.Fprintf(&b, "  %s  %s  found=%s  %s\n", i.Bus, i.Name, strings.Join(i.Found, ","), i.Note)
	}
	fmt.Fprintf(&b, "Parallel ports: %s\n", strings.Join(f.Parallel, ", "))
	for _, m := range f.Matches {
		fmt.Fprintf(&b, "Match (%s): driver %s %v. %s\n", m.Sure, m.Driver, m.Settings, m.Why)
	}
	for _, n := range f.Notes {
		fmt.Fprintf(&b, "Note: %s\n", n)
	}
	return b.String()
}
