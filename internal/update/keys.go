// Package update installs new versions of Gatehouse from a signed package, without an ISO.
//
// A release carries gatehouse-update-<version>.tar.gz and a signature file next to it. The
// package is trusted only if the signature matches the public key built into the program
// (update-key.pub), and every file inside matches the hash in its manifest. The root helper
// checks again before it installs anything, so the daemon is never trusted to have done so.
package update

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

//go:embed update-key.pub
var embeddedKey string

// ErrNoKey means this build has no update key, so no update can be trusted.
var ErrNoKey = errors.New("this build has no update signing key yet; updates are switched off until one is added")

// EmbeddedKey returns the public key built into the program.
func EmbeddedKey() (ed25519.PublicKey, error) {
	k := strings.TrimSpace(embeddedKey)
	if k == "" || k == "unset" {
		return nil, ErrNoKey
	}
	return ParsePublicKey(k)
}

func readString(b []byte) (s, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("short key")
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, errors.New("short key")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// ParsePublicKey reads the one-line "ssh-ed25519 AAAA..." form that ssh-keygen writes.
func ParsePublicKey(line string) (ed25519.PublicKey, error) {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return nil, errors.New("the update key must be an ssh-ed25519 public key")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return nil, errors.New("the update key is damaged")
	}
	kind, rest, err := readString(blob)
	if err != nil || string(kind) != "ssh-ed25519" {
		return nil, errors.New("the update key is damaged")
	}
	pub, _, err := readString(rest)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("the update key is damaged")
	}
	return ed25519.PublicKey(pub), nil
}

// ParsePrivateKey reads an unencrypted OpenSSH ed25519 private key, as ssh-keygen -t ed25519 -N ""
// writes it. Used only by the release build to sign packages.
func ParsePrivateKey(pemText string) (ed25519.PrivateKey, error) {
	var b64 strings.Builder
	in := false
	for _, l := range strings.Split(pemText, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "-----BEGIN OPENSSH PRIVATE KEY"):
			in = true
		case strings.HasPrefix(l, "-----END"):
			in = false
		case in:
			b64.WriteString(l)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		return nil, errors.New("that is not an OpenSSH private key")
	}
	magic := []byte("openssh-key-v1\x00")
	if !bytes.HasPrefix(raw, magic) {
		return nil, errors.New("that is not an OpenSSH private key")
	}
	b := raw[len(magic):]
	cipher, b, err := readString(b)
	if err != nil || string(cipher) != "none" {
		return nil, errors.New("the signing key must have no passphrase")
	}
	if _, b, err = readString(b); err != nil { // kdf
		return nil, err
	}
	if _, b, err = readString(b); err != nil { // kdf options
		return nil, err
	}
	if len(b) < 4 || binary.BigEndian.Uint32(b) != 1 {
		return nil, errors.New("the signing key file must hold exactly one key")
	}
	b = b[4:]
	if _, b, err = readString(b); err != nil { // public key blob
		return nil, err
	}
	priv, _, err := readString(b)
	if err != nil || len(priv) < 8 {
		return nil, errors.New("the signing key is damaged")
	}
	priv = priv[8:] // two check integers
	kind, priv, err := readString(priv)
	if err != nil || string(kind) != "ssh-ed25519" {
		return nil, errors.New("the signing key must be an ed25519 key")
	}
	if _, priv, err = readString(priv); err != nil { // public
		return nil, err
	}
	sk, _, err := readString(priv)
	if err != nil || len(sk) != ed25519.PrivateKeySize {
		return nil, errors.New("the signing key is damaged")
	}
	return ed25519.PrivateKey(sk), nil
}

// PublicLine renders a public key the way ParsePublicKey reads it.
func PublicLine(pub ed25519.PublicKey) string {
	var blob []byte
	add := func(s []byte) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(s)))
		blob = append(blob, n[:]...)
		blob = append(blob, s...)
	}
	add([]byte("ssh-ed25519"))
	add(pub)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
}

// Sign returns the signature of data as base64 text.
func Sign(priv ed25519.PrivateKey, data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
}

// VerifySignature checks a base64 signature of data against pub.
func VerifySignature(pub ed25519.PublicKey, data []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, data, sig) {
		return fmt.Errorf("the update's signature is not valid, so it was not installed")
	}
	return nil
}
