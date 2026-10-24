package main

import (
	"crypto/ed25519"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gatehouse/internal/fw"
	"gatehouse/internal/netd"
	"gatehouse/internal/update"
)

// updateApply installs a staged update. It runs as its own service, started by the helper, because
// installing it restarts both the console and the helper.
func updateApply() {
	pub, err := update.EmbeddedKey()
	if err != nil {
		log.Fatalf("update: %v", err)
	}
	var p *update.Package // nil when only the operating system is being updated
	if _, err := os.Stat(filepath.Join(netd.UpdateStage, "system-only")); err != nil {
		data, err := os.ReadFile(filepath.Join(netd.UpdateStage, "package.tar.gz"))
		if err != nil {
			log.Fatalf("update: %v", err)
		}
		sig, _ := os.ReadFile(filepath.Join(netd.UpdateStage, "package.sig"))
		p, err = update.Open(data, string(sig), pub)
		if err != nil {
			log.Fatalf("update: %v", err)
		}
	}
	from := strings.TrimSpace(readFile(netd.ReleaseFile))
	fw.ConsolePortFile = "/var/lib/gatehouse/console.port"
	env := update.Env{
		StateDir: netd.UpdateState,
		Run: func(name string, args ...string) error {
			out, err := exec.Command(name, args...).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
		System:       netd.SystemUpgrade,
		Health:       consoleAnswers,
		Sleep:        time.Sleep,
		Now:          time.Now,
		Services:     []string{"gatehouse-netd", "gatehouse", "gatehouse-screen"},
		Wait:         2 * time.Minute,
		RollbackWait: time.Minute,
	}
	res := update.Apply(env, p, from)
	os.RemoveAll(netd.UpdateStage)
	if res.OK {
		log.Printf("update: now running %s (system packages changed: %d)", res.To, systemCount(res))
	} else {
		log.Printf("update: %s", res.Error)
	}
}

// readPackageList reads one package name per line; blank lines and # comments are ignored.
func readPackageList(path string) []string {
	if path == "" {
		return nil
	}
	var out []string
	for _, l := range strings.Split(readFile(path), "\n") {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func systemCount(r update.Result) int {
	if r.System == nil {
		return 0
	}
	return r.System.Packages
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// consoleAnswers is true when the console answers over the loopback on its saved port.
func consoleAnswers() bool {
	hc := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // our own console on the loopback
	port := fw.SavedConsolePort()
	for _, scheme := range []string{"https", "http"} {
		resp, err := hc.Get(fmt.Sprintf("%s://127.0.0.1:%d/api/health", scheme, port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
	}
	return false
}

// updateBuild makes and signs an update package. The release build runs it; nobody else needs to.
func updateBuild(args []string) {
	fs := flag.NewFlagSet("update-build", flag.ExitOnError)
	version := fs.String("version", "", "version of the package")
	notes := fs.String("notes", "", "file with the release notes")
	bin := fs.String("bin", "", "the program")
	units := fs.String("units", "", "folder with the service files")
	keyFile := fs.String("key", "", "the signing key (OpenSSH, no passphrase)")
	out := fs.String("out", ".", "where to write the package and its signature")
	pkgs := fs.String("packages", "", "file listing the Debian packages this release needs, one per line")
	fs.Parse(args)
	if *version == "" || *bin == "" || *keyFile == "" {
		log.Fatal("update-build: -version, -bin and -key are required")
	}
	priv, err := update.ParsePrivateKey(readFile(*keyFile))
	if err != nil {
		log.Fatalf("update-build: %v", err)
	}
	files := map[string]string{update.BinaryDest: *bin}
	modes := map[string]uint32{update.BinaryDest: 0o755}
	if *units != "" {
		ents, _ := os.ReadDir(*units)
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".service") {
				files["/etc/systemd/system/"+e.Name()] = filepath.Join(*units, e.Name())
			}
		}
	}
	data, err := update.BuildWith(*version, readFile(*notes), time.Now().UTC().Format("2006-01-02"), readPackageList(*pkgs), files, modes)
	if err != nil {
		log.Fatalf("update-build: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	sig := update.Sign(priv, data)
	if _, err := update.Open(data, sig, pub); err != nil {
		log.Fatalf("update-build: the package does not open with its own key: %v", err)
	}
	// A package signed with a key the program does not know would be refused by every machine.
	if built, err := update.EmbeddedKey(); err != nil {
		log.Fatalf("update-build: %v. Put your public key in internal/update/update-key.pub first", err)
	} else if !built.Equal(pub) {
		log.Fatal("update-build: the signing key is not the one built into this program (internal/update/update-key.pub); machines would refuse this update")
	}
	name := filepath.Join(*out, "gatehouse-update-"+*version+".tar.gz")
	if err := os.WriteFile(name, data, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(name+".sig", []byte(sig+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println(name)
}
