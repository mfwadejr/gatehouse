// Command gatehouse is the appliance program. Sub-commands select the role.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gatehouse/internal/app"
	"gatehouse/internal/auth"
	"gatehouse/internal/crypt"
	"gatehouse/internal/fw"
	"gatehouse/internal/installer"
	"gatehouse/internal/netcfg"
	"gatehouse/internal/netd"
	"gatehouse/internal/privsep"
	"gatehouse/internal/seed"
	"gatehouse/internal/store"
	"gatehouse/internal/tlsutil"
)

const usage = `gatehouse <command>

Commands:
  daemon     run the DNS, DHCP and console services
  netd       run the privileged network helper (firewall, interfaces, WAN monitor)
  installer  run the installer (on the live USB image only)
  textinstaller  run the text-mode installer (boot menu: text / serial console)
  user       add an administrator or reset a password (run on the device itself)
  recover    local recovery screen: new password, turn off two-step sign-in, reset the firewall
  seed       store the network chosen in the installer (used by the installer)
  rollback   undo a firewall or network change that was not confirmed (started automatically)
  version    print the version

Run "gatehouse daemon -h" for options.`

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "daemon":
		daemon(os.Args[2:])
	case "netd":
		runNetd(os.Args[2:])
	case "installer":
		runInstaller(os.Args[2:])
	case "textinstaller":
		installer.PinMemory()
		if err := installer.RunTextInstaller(); err != nil {
			log.Fatalf("textinstaller: %v", err)
		}
	case "ssh-auth":
		sshAuth()
	case "screen":
		installer.RunStatus("443")
	case "update-apply":
		updateApply()
	case "update-build":
		updateBuild(os.Args[2:])
	case "recover":
		recoverCmd(os.Args[2:])
	case "user":
		userCmd(os.Args[2:])
	case "seed":
		seedCmd(os.Args[2:])
	case "rollback":
		rollback(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", os.Args[1], usage)
		os.Exit(2)
	}
}

func daemon(args []string) {
	cfg := app.DefaultConfig()
	cfg.Version = version
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	fs.StringVar(&cfg.DataDir, "data", cfg.DataDir, "directory for databases and keys")
	fs.StringVar(&cfg.WebAddr, "web", cfg.WebAddr, "console listen address")
	dnsAddrs := fs.String("dns", strings.Join(cfg.DNSAddrs, ","), "DNS listen addresses, comma separated")
	fs.StringVar(&cfg.DHCPAddr, "dhcp", cfg.DHCPAddr, "DHCP listen address, empty to disable")
	plain := fs.Bool("http", false, "serve the console without HTTPS (testing only)")
	fs.StringVar(&cfg.TLSCert, "cert", "", "TLS certificate file")
	fs.StringVar(&cfg.TLSKey, "key", "", "TLS key file")
	sock := fs.String("netd", "/run/gatehouse/netd.sock", "network helper socket")
	fs.Parse(args)
	cfg.DNSAddrs = strings.Split(*dnsAddrs, ",")
	cfg.Static = staticHandler()
	if cfg.TLSCert == "" && !*plain {
		host, _ := os.Hostname()
		c, k, err := tlsutil.Ensure(filepath.Join(cfg.DataDir, "tls"), host)
		if err != nil {
			log.Fatalf("certificate: %v", err)
		}
		cfg.TLSCert, cfg.TLSKey = c, k
	}
	for i := 0; i < 50 && dirExists(filepath.Dir(*sock)); i++ { // the helper starts first, but give it a moment
		if _, err := os.Stat(*sock); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := os.Stat(*sock); err == nil {
		c := &privsep.Client{Path: *sock}
		cfg.FW, cfg.Net = c, c
	} else {
		log.Printf("network helper socket %s not found; firewall and network editing are disabled", *sock)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("start: %v", err)
	}
	defer a.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fw.ConsolePortFile = filepath.Join(cfg.DataDir, "console.port")
	go watchdog(ctx, a, cfg)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}
}

// rollback is the independent safety timer. It reads only the state directory.
func rollback(args []string) {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	dir := fs.String("dir", "/var/lib/gatehouse/fw", "firewall state directory")
	kind := fs.String("kind", "fw", "what to undo: fw or net")
	token := fs.String("token", "", "token of the change to undo")
	deadline := fs.Int64("deadline", 0, "unix time at which to act")
	netns := fs.String("netns", "", "network namespace (testing)")
	fs.Parse(args)
	if *token == "" {
		log.Fatal("rollback: -token is required")
	}
	if *kind == "net" {
		in := netcfg.NewInstaller()
		in.StateDir = *dir
		if err := netcfg.RunWatchdog(in, time.Unix(*deadline, 0), *token); err != nil {
			log.Fatalf("rollback: %v", err)
		}
		return
	}
	a := fw.NewApplier(*dir)
	a.Netns = *netns
	if err := fw.RunWatchdog(*dir, time.Unix(*deadline, 0), *token, a); err != nil {
		log.Fatalf("rollback: %v", err)
	}
}

// runNetd is the root helper. It listens on a unix socket for the daemon.
func runNetd(args []string) {
	fs := flag.NewFlagSet("netd", flag.ExitOnError)
	sock := fs.String("socket", "/run/gatehouse/netd.sock", "socket the daemon connects to")
	group := fs.String("group", "gatehouse", "group allowed to use the socket")
	state := fs.String("state", "/var/lib/gatehouse", "state directory")
	allow := fs.Int("uid", -1, "user id of the daemon (checked on every connection)")
	uname := fs.String("user", "", "name of the daemon's account; used instead of -uid")
	fs.Parse(args)
	if *uname != "" {
		u, err := user.Lookup(*uname)
		if err != nil {
			log.Fatalf("netd: user %q: %v", *uname, err)
		}
		n, err := strconv.Atoi(u.Uid)
		if err != nil {
			log.Fatalf("netd: user %q has an unusable id", *uname)
		}
		*allow = n
	}

	gid := lookupGID(*group)
	fw.ConsolePortFile = *state + "/console.port"
	ap := fw.NewApplier(*state + "/fw")
	ap.Baseline = fw.FirstBootBaseline
	if err := ap.Boot(); err != nil {
		log.Printf("netd: firewall boot: %v", err)
	}
	inst := netcfg.NewInstaller()
	inst.StateDir = *state + "/net"
	inst.Watchdog = netcfg.StartWatchdogProcess
	svc := netd.New(inst, *state+"/netd")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := svc.Start(ctx); err != nil {
		log.Fatalf("netd: %v", err)
	}
	defer svc.Stop()

	if err := os.MkdirAll(filepath.Dir(*sock), 0o755); err != nil {
		log.Fatalf("netd: %v", err)
	}
	l, err := privsep.Listen(*sock, gid)
	if err != nil {
		log.Fatalf("netd: %v", err)
	}
	srv := &privsep.Server{FW: ap, Net: svc}
	if *allow >= 0 {
		srv.AllowUID = []uint32{uint32(*allow)}
	}
	go func() { <-ctx.Done(); l.Close() }()
	log.Printf("netd: listening on %s", *sock)
	if err := srv.Serve(l); err != nil && ctx.Err() == nil {
		log.Fatalf("netd: %v", err)
	}
}

func lookupGID(name string) int {
	g, err := user.LookupGroup(name)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1
	}
	return n
}

func dirExists(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

// userCmd manages accounts directly in the database. The password is read from standard
// input so it never appears in a process list or shell history.
func userCmd(args []string) {
	if len(args) < 1 || (args[0] != "add" && args[0] != "passwd") {
		fmt.Fprintln(os.Stderr, "usage: gatehouse user add|passwd -data DIR -name NAME   (password on stdin)")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("user", flag.ExitOnError)
	data := fs.String("data", "/var/lib/gatehouse", "data directory")
	name := fs.String("name", "", "account name")
	requireMFA := fs.Bool("require-mfa", false, "require two-step sign-in for every account")
	fs.Parse(args[1:])
	if *name == "" {
		log.Fatal("user: -name is required")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	pw := strings.TrimRight(line, "\r\n")
	if err != nil && pw == "" {
		log.Fatal("user: read the password from standard input")
	}
	if err := os.MkdirAll(*data, 0o750); err != nil {
		log.Fatalf("user: %v", err)
	}
	db, err := store.OpenConfig(filepath.Join(*data, "config.db"))
	if err != nil {
		log.Fatalf("user: %v", err)
	}
	defer db.Close()
	box, err := crypt.LoadOrCreate(filepath.Join(*data, "secret.key"))
	if err != nil {
		log.Fatalf("user: %v", err)
	}
	svc := auth.New(db, box)
	if *requireMFA {
		if err := svc.SetMFARequired(true); err != nil {
			log.Fatalf("user: %v", err)
		}
	}
	switch args[0] {
	case "add":
		if _, err := svc.CreateUser(*name, pw, "admin"); err != nil {
			log.Fatalf("user: %v", err)
		}
		os.Remove(filepath.Join(*data, "setup-code"))
	case "passwd":
		u, err := svc.UserByName(*name)
		if err != nil {
			log.Fatalf("user: no such account")
		}
		if err := svc.SetPassword(u.ID, pw); err != nil {
			log.Fatalf("user: %v", err)
		}
	}
	fmt.Println("done")
}

// runInstaller serves the installer console from the live image. The one-time code is shown
// only on this machine's screen.
func runInstaller(args []string) {
	installer.PinMemory()
	fs := flag.NewFlagSet("installer", flag.ExitOnError)
	addr := fs.String("addr", ":443", "HTTPS listen address")
	fs.Parse(args)
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	n := int(binary.BigEndian.Uint32(b)) % 100000000
	if n < 0 {
		n = -n
	}
	code := fmt.Sprintf("%04d-%04d", n/10000, n%10000)
	os.MkdirAll("/run/gatehouse", 0o755)
	os.WriteFile("/run/gatehouse/installer-code", []byte(code+"\n"), 0o600)
	cert, key, err := tlsutil.Ensure("/run/gatehouse/tls", "gatehouse")
	if err != nil {
		log.Fatalf("installer: certificate: %v", err)
	}
	srv := &installer.Server{In: installer.New(), Code: code, Web: staticHandler()}
	_, port, _ := net.SplitHostPort(*addr)
	go installer.RunScreen(code, port)
	// Only private addresses may reach the installer, so plugging the internet cable in first
	// does not expose it.
	if l80, err := net.Listen("tcp", ":80"); err == nil {
		go http.Serve(installer.PrivateOnly(l80), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Host
			if i := strings.LastIndex(h, ":"); i >= 0 {
				h = h[:i]
			}
			http.Redirect(w, r, "https://"+h+r.URL.RequestURI(), http.StatusMovedPermanently)
		}))
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("installer: %v", err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(hs.ServeTLS(installer.PrivateOnly(ln), cert, key))
}

// seedCmd stores the network chosen in the installer. The choice arrives as JSON on standard
// input because it may include an internet provider password.
func seedCmd(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	data := fs.String("data", "/var/lib/gatehouse", "data directory")
	fs.Parse(args)
	var n seed.Network
	dec := json.NewDecoder(os.Stdin)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil {
		log.Fatalf("seed: read the network choice as JSON on standard input: %v", err)
	}
	if err := os.MkdirAll(*data, 0o750); err != nil {
		log.Fatalf("seed: %v", err)
	}
	db, err := store.OpenConfig(filepath.Join(*data, "config.db"))
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer db.Close()
	box, err := crypt.LoadOrCreate(filepath.Join(*data, "secret.key"))
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	if err := seed.Apply(db, box, &n); err != nil {
		log.Fatalf("seed: %v", err)
	}
	fmt.Println("done")
}

// recoverCmd is the screen for someone locked out who is standing at the machine. It runs as root on a
// local console (Alt+F2), never over the network.
func recoverCmd(args []string) {
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	data := fs.String("data", "/var/lib/gatehouse", "data directory")
	fs.Parse(args)
	// This screen starts with the machine, before the console has made its own files. It must never
	// create them: files made here are owned by root and the console could not open them. So the
	// database is opened only when something needs it, and only if it already exists.
	fixOwner := func() {
		// Hand back what the console uses. The helper's own folders stay with root.
		ents, _ := os.ReadDir(*data)
		for _, e := range ents {
			switch e.Name() {
			case "fw", "net", "netd":
				continue
			}
			exec.Command("chown", "-R", "gatehouse:gatehouse", filepath.Join(*data, e.Name())).Run()
		}
	}
	var db *store.DB
	var svc *auth.Service
	open := func() error {
		if svc != nil {
			return nil
		}
		cfgPath, keyPath := filepath.Join(*data, "config.db"), filepath.Join(*data, "secret.key")
		if _, err := os.Stat(cfgPath); err != nil {
			return errors.New("Gatehouse has not been set up yet")
		}
		if _, err := os.Stat(keyPath); err != nil {
			return errors.New("Gatehouse has not been set up yet")
		}
		d, err := store.OpenConfig(cfgPath)
		if err != nil {
			return err
		}
		box, err := crypt.LoadOrCreate(keyPath)
		if err != nil {
			d.Close()
			return err
		}
		db, svc = d, auth.New(d, box)
		return nil
	}
	defer func() {
		if db != nil {
			db.Close()
			fixOwner()
		}
	}()
	env := installer.RecoveryEnv{
		Users: func() []string {
			if open() != nil {
				return nil
			}
			rows, err := db.Query(`SELECT username FROM users ORDER BY id`)
			if err != nil {
				return nil
			}
			defer rows.Close()
			var out []string
			for rows.Next() {
				var n string
				rows.Scan(&n)
				out = append(out, n)
			}
			return out
		},
		SetPassword: func(name, pw string) error {
			if err := open(); err != nil {
				return err
			}
			u, err := svc.UserByName(name)
			if err != nil {
				return errors.New("no such account")
			}
			defer fixOwner()
			return svc.SetPassword(u.ID, pw)
		},
		ClearTwoStep: func(name string) error {
			if err := open(); err != nil {
				return err
			}
			u, err := svc.UserByName(name)
			if err != nil {
				return errors.New("no such account")
			}
			defer fixOwner()
			return svc.ClearTOTP(u.ID)
		},
		ResetFirewall: func() error {
			fw.ConsolePortFile = filepath.Join(*data, "console.port")
			dir := filepath.Join(*data, "fw")
			os.Remove(filepath.Join(dir, "lastgood.nft"))
			ap := fw.NewApplier(dir)
			ap.Baseline = fw.FirstBootBaseline
			return ap.Rollback()
		},
		ResetAccess: func() error {
			// Back to port 443, and anyone on a management network may manage again.
			if err := open(); err != nil {
				return err
			}
			defer fixOwner()
			for _, k := range []string{"console.port", "console.port.pending", "mgmt.allow", "ssh.enabled"} {
				db.Exec(`DELETE FROM settings WHERE key=?`, k)
			}
			os.WriteFile(filepath.Join(*data, "console.port"), []byte("443\n"), 0o644)
			os.Remove(filepath.Join(*data, "fw", "lastgood.nft"))
			fw.ConsolePortFile = filepath.Join(*data, "console.port")
			ap := fw.NewApplier(filepath.Join(*data, "fw"))
			ap.Baseline = fw.FirstBootBaseline
			if err := ap.Rollback(); err != nil {
				return err
			}
			exec.Command("systemctl", "disable", "--now", "ssh.service").Run() // remote login goes back to off
			exec.Command("systemctl", "restart", "gatehouse").Run()
			return nil
		},
		ResetToDefault: func(level string) (string, error) {
			if err := open(); err != nil {
				return "", err
			}
			defer fixOwner()
			dir := filepath.Join(*data, "backups")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
			saved := filepath.Join(dir, "before-reset-"+time.Now().Format("20060102-150405")+".db")
			if _, err := db.Exec(`VACUUM INTO ?`, saved); err != nil {
				return "", fmt.Errorf("could not save a copy of the settings first: %w", err)
			}
			if err := store.ResetConfig(db, level); err != nil {
				return "", err
			}
			if level == store.ResetFactory {
				if ldb, err := store.OpenLogs(filepath.Join(*data, "logs.db")); err == nil {
					store.ResetLogs(ldb)
					ldb.Close()
				}
				os.RemoveAll(filepath.Join(*data, "update"))
			}
			// The running console puts the empty network and safe firewall in place when it starts.
			os.WriteFile(filepath.Join(*data, app.ResetMarker), []byte("1\n"), 0o600)
			exec.Command("systemctl", "restart", "gatehouse").Run()
			return saved, nil
		},
		Diagnose: diagnose,
		Services: func() string {
			if open() != nil {
				return "Gatehouse has not been set up yet."
			}
			return app.ServicesText(db, "")
		},
	}
	if err := installer.RunRecoveryConsole(env); err != nil {
		log.Fatalf("recover: %v", err)
	}
	// "Leave recovery" goes back to the status screen. This program is restarted by systemd, so
	// without this the same menu would simply appear again.
	showStatusScreen()
}

// diagnose says what is running and shows the last lines the programs wrote.
func diagnose() string {
	var b strings.Builder
	for _, u := range []string{"gatehouse-netd", "gatehouse", "gatehouse-screen", "systemd-networkd"} {
		out, _ := exec.Command("systemctl", "is-active", u).Output()
		st := strings.TrimSpace(string(out))
		if st == "" {
			st = "unknown"
		}
		fmt.Fprintf(&b, "%-18s %s\n", u, st)
	}
	b.WriteString("\nLast messages from Gatehouse:\n")
	out, _ := exec.Command("journalctl", "-u", "gatehouse", "-u", "gatehouse-netd", "-n", "10", "--no-pager", "-o", "cat").CombinedOutput()
	b.WriteString(strings.TrimSpace(string(out)))
	return b.String()
}

// sshAuth is run by sshd (through PAM) to check an administrator's password. The user name and the
// address come from the environment, the password from standard input. It asks the console on this
// machine, so the same limits and the same accounts apply as for signing in there. Exit 0 means yes.
func sshAuth() {
	user, from := os.Getenv("PAM_USER"), os.Getenv("PAM_RHOST")
	pw, _ := bufio.NewReader(os.Stdin).ReadString(0)
	pw = strings.TrimRight(pw, "\x00\n")
	if user == "" || pw == "" {
		os.Exit(1)
	}
	port := "443"
	if b, err := os.ReadFile("/var/lib/gatehouse/console.port"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 && n < 65536 {
			port = strconv.Itoa(n)
		}
	}
	body, _ := json.Marshal(map[string]string{"user": user, "password": pw, "from": from})
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // the machine itself, on its own loopback address
	for _, scheme := range []string{"https", "http"} {
		resp, err := hc.Post(scheme+"://127.0.0.1:"+port+"/api/auth/ssh-verify", "application/json", bytes.NewReader(body))
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			os.Exit(0)
		}
		break
	}
	time.Sleep(time.Second) // slows down guessing
	os.Exit(1)
}
