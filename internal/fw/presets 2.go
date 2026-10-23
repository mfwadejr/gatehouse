package fw

// Preset describes a kind of network in the plain words the console uses.
type Preset struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Internet    bool   `json:"internet"`
	IntraAllow  bool   `json:"intra_allow"`
	Management  bool   `json:"management"`
	Recommended string `json:"recommended"`
}

// Presets are what the "add a network" flow offers.
func Presets() []Preset {
	return []Preset{
		{KindTrusted, "Trusted", "Your own computers and phones. Can reach the internet and every other network. Nothing else can start a connection to it.", true, true, true, "Home or office computers"},
		{KindPrivate, "Private", "Can reach the internet. Cannot reach your other networks.", true, true, false, "Shared spaces, family devices"},
		{KindGuest, "Guest", "Internet only. Cannot reach any other network or manage this device.", true, false, false, "Visitors"},
		{KindIsolated, "Isolated", "No internet and no other networks. Add internet access per device group if needed.", false, true, false, "Cameras, smart-home gadgets"},
		{KindCustom, "Custom", "Starts with nothing allowed. You write the exceptions.", false, true, false, "Anything unusual"},
	}
}

// ApplyPreset sets a zone's switches from its kind.
func ApplyPreset(z *Zone) {
	for _, p := range Presets() {
		if p.Kind == z.Kind {
			z.Internet, z.IntraAllow, z.Management = p.Internet, p.IntraAllow, p.Management
		}
	}
	z.Gateway = true
	z.DNSForce = true
}

// DefaultModel is what the installer starts from: one trusted network, one internet connection.
func DefaultModel(wan []string, lan []string) *Model {
	trusted := Zone{Name: "Home", Kind: KindTrusted, Ifaces: lan, SSH: false}
	ApplyPreset(&trusted)
	return &Model{
		Zones: []Zone{
			trusted,
			{Name: "Internet", Kind: KindWan, Ifaces: wan, Gateway: true},
		},
		Groups:       map[string][]string{},
		ConsolePorts: append([]int(nil), DefaultConsolePorts...),
		DNSRedirect:  true,
		BlockDoT:     true,
	}
}
