package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// File is one thing the package installs.
type File struct {
	Path   string `json:"path"` // inside the package
	Dest   string `json:"dest"` // where it goes on the machine
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

// Manifest describes a package. It is signed along with everything else.
type Manifest struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Notes   string `json:"notes"`
	Files   []File `json:"files"`
	// Packages are Debian packages this release needs, installed in the same run before Gatehouse
	// itself. The list is part of the signed package, so it cannot be changed from outside.
	Packages []string `json:"packages,omitempty"`
}

const (
	maxPackage = 200 << 20
	maxFile    = 150 << 20
	BinaryDest = "/usr/local/bin/gatehouse"
)

var reUnit = regexp.MustCompile(`^/etc/systemd/system/gatehouse[a-z0-9@._-]*\.service$`)

// allowedDest says what an update may overwrite: the program and Gatehouse's own services.
func allowedDest(d string) bool { return d == BinaryDest || reUnit.MatchString(d) }

// maxPackages keeps a signed package from asking for an unreasonable amount of software.
const maxPackages = 40

var reDebPackage = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{1,99}$`)

var reVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}([-+][0-9A-Za-z.-]+)?$`)

// ParseVersion reads 1.2.3 (a leading v is allowed); anything else counts as 0.0.0.
func ParseVersion(s string) [4]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var out [4]int
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	for i, p := range strings.Split(s, ".") {
		if i > 3 {
			break
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return [4]int{}
		}
		out[i] = n
	}
	return out
}

// Compare is -1, 0 or 1 as a is older than, the same as, or newer than b.
func Compare(a, b string) int {
	x, y := ParseVersion(a), ParseVersion(b)
	for i := range x {
		switch {
		case x[i] < y[i]:
			return -1
		case x[i] > y[i]:
			return 1
		}
	}
	return 0
}

// Package is a package that has been checked.
type Package struct {
	Manifest Manifest
	Files    map[string][]byte // by path inside the package
}

// Open checks a package's signature and contents. data is the whole .tar.gz.
func Open(data []byte, sig string, pub ed25519.PublicKey) (*Package, error) {
	if len(data) == 0 || len(data) > maxPackage {
		return nil, errors.New("the update file is empty or too large")
	}
	if err := VerifySignature(pub, data, sig); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the update file is damaged")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	p := &Package{Files: map[string][]byte{}}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("the update file is damaged")
		}
		if h.Typeflag != tar.TypeReg {
			continue // folders carry nothing; links and devices are never accepted
		}
		name := path.Clean(h.Name)
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "..") || strings.Contains(name, "/../") {
			return nil, errors.New("the update file contains an unsafe path")
		}
		if h.Size > maxFile || total+h.Size > maxPackage {
			return nil, errors.New("the update file is too large")
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxFile+1))
		if err != nil || int64(len(b)) != h.Size {
			return nil, errors.New("the update file is damaged")
		}
		total += h.Size
		p.Files[name] = b
	}
	mb, ok := p.Files["manifest.json"]
	if !ok || json.Unmarshal(mb, &p.Manifest) != nil {
		return nil, errors.New("the update has no readable manifest")
	}
	m := &p.Manifest
	if !reVersion.MatchString(m.Version) {
		return nil, errors.New("the update has an invalid version")
	}
	if len(m.Packages) > maxPackages {
		return nil, errors.New("the update asks for too many system packages")
	}
	seenPkg := map[string]bool{}
	for _, name := range m.Packages {
		if !reDebPackage.MatchString(name) {
			return nil, fmt.Errorf("the update names a system package that is not valid: %q", name)
		}
		if seenPkg[name] {
			return nil, errors.New("the update lists a system package twice")
		}
		seenPkg[name] = true
	}
	seenBinary := false
	seen := map[string]bool{}
	for _, f := range m.Files {
		if !allowedDest(f.Dest) {
			return nil, fmt.Errorf("the update tries to replace %s, which updates may not change", f.Dest)
		}
		if seen[f.Dest] {
			return nil, errors.New("the update lists a file twice")
		}
		seen[f.Dest] = true
		b, ok := p.Files[path.Clean(f.Path)]
		if !ok {
			return nil, fmt.Errorf("the update is missing %s", f.Path)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != strings.ToLower(f.SHA256) {
			return nil, fmt.Errorf("%s does not match its fingerprint", f.Path)
		}
		if f.Mode&^0o755 != 0 {
			return nil, fmt.Errorf("%s has unexpected permissions", f.Path)
		}
		if f.Dest == BinaryDest {
			seenBinary = true
			if len(b) < 4 || string(b[:4]) != "\x7fELF" {
				return nil, errors.New("the program in the update is not a program")
			}
		}
	}
	if !seenBinary {
		return nil, errors.New("the update does not contain the program")
	}
	return p, nil
}

// Build makes a package from files on disk and signs nothing: the caller signs the result.
// files maps destination to source path.
func Build(version, notes, date string, files map[string]string, modes map[string]uint32) ([]byte, error) {
	return BuildWith(version, notes, date, nil, files, modes)
}

// BuildWith is Build for a release that also needs Debian packages installed.
func BuildWith(version, notes, date string, packages []string, files map[string]string, modes map[string]uint32) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	var m Manifest
	m.Version, m.Notes, m.Date = version, notes, date
	m.Packages = packages
	dests := make([]string, 0, len(files))
	for d := range files {
		dests = append(dests, d)
	}
	sortStrings(dests)
	put := func(name string, b []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}
	for i, d := range dests {
		b, err := os.ReadFile(files[d])
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		mode := modes[d]
		if mode == 0 {
			mode = 0o644
		}
		name := fmt.Sprintf("files/%d-%s", i, filepath.Base(d))
		m.Files = append(m.Files, File{Path: name, Dest: d, SHA256: hex.EncodeToString(sum[:]), Mode: mode})
		if err := put(name, b, int64(mode)); err != nil {
			return nil, err
		}
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := put("manifest.json", mb, 0o644); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
