// Package auth implements accounts, passwords, TOTP, recovery codes and sessions.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const pbkdf2Iterations = 600_000

// HashPassword returns "pbkdf2-sha256$iterations$salt$hash".
func HashPassword(pw string) (string, error) {
	return hashWith(pw, pbkdf2Iterations)
}

func hashWith(pw string, iter int) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a stored hash in constant time.
func VerifyPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Policy is the rule for every new password NopeWarden sets or accepts: console accounts, the first
// account, the recovery console, the backup file and the copies sent off the machine.
type Policy struct {
	Min    int  `json:"min"`    // shortest allowed, 8 to 64
	Upper  bool `json:"upper"`  // needs a capital letter
	Lower  bool `json:"lower"`  // needs a lower case letter
	Digit  bool `json:"digit"`  // needs a number
	Symbol bool `json:"symbol"` // needs a symbol
	Common bool `json:"common"` // refuse very common passwords
}

// DefaultPolicy is what a new box starts with: eight characters, no forced types, common passwords refused.
func DefaultPolicy() Policy { return Policy{Min: 8, Common: true} }

// Clean puts a policy into its allowed range.
func (p Policy) Clean() Policy {
	if p.Min < 8 {
		p.Min = 8
	}
	if p.Min > 64 {
		p.Min = 64
	}
	return p
}

// Describe lists the rules in plain words, for the page and for error messages.
func (p Policy) Describe() string {
	p = p.Clean()
	parts := []string{fmt.Sprintf("at least %d characters", p.Min)}
	var kinds []string
	if p.Upper {
		kinds = append(kinds, "a capital letter")
	}
	if p.Lower {
		kinds = append(kinds, "a small letter")
	}
	if p.Digit {
		kinds = append(kinds, "a number")
	}
	if p.Symbol {
		kinds = append(kinds, "a symbol")
	}
	if len(kinds) > 0 {
		parts = append(parts, "with "+strings.Join(kinds, ", "))
	}
	if p.Common {
		parts = append(parts, "not a very common password")
	}
	return strings.Join(parts, ", ")
}

// Check says what is wrong with a password, naming the first rule it breaks. The user name may be empty.
func (p Policy) Check(pw, username string) error {
	p = p.Clean()
	n := len([]rune(pw))
	if n < p.Min {
		return fmt.Errorf("the password is too short: %d of the %d characters needed", n, p.Min)
	}
	if n > 256 {
		return errors.New("the password is too long")
	}
	var up, lo, di, sy bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			lo = true
		case unicode.IsDigit(r):
			di = true
		default:
			sy = true
		}
	}
	switch {
	case p.Upper && !up:
		return errors.New("the password needs a capital letter")
	case p.Lower && !lo:
		return errors.New("the password needs a small letter")
	case p.Digit && !di:
		return errors.New("the password needs a number")
	case p.Symbol && !sy:
		return errors.New("the password needs a symbol such as ! or #")
	}
	if username != "" && strings.EqualFold(pw, username) {
		return errors.New("the password must not match the username")
	}
	if p.Common && IsCommonPassword(pw) {
		return errors.New("that password is one of the most common ones. Pick something less guessable, such as a few unrelated words")
	}
	return nil
}

// CheckPasswordPolicy checks a password against the default policy. Code that can read the box's own
// settings uses Service.CheckPassword instead.
func CheckPasswordPolicy(pw, username string) error { return DefaultPolicy().Check(pw, username) }

// IsCommonPassword reports whether a password is on the built-in list of very common ones, ignoring
// case and a trailing run of digits or symbols that people add ("Password1!").
func IsCommonPassword(pw string) bool {
	l := strings.ToLower(pw)
	if commonPasswords[l] {
		return true
	}
	t := strings.TrimRight(l, "0123456789!@#$%^&*._-")
	return t != "" && t != l && commonPasswords[t]
}

var commonPasswords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`password passw0rd p@ssword p@ssw0rd password1 password123 123456 1234567 12345678 123456789
1234567890 12345678910 11111111 00000000 87654321 qwerty qwertyuiop qwerty123 qwertyui asdfghjkl asdfgh zxcvbnm 1q2w3e4r
1qaz2wsx abc123 abcd1234 abcdefgh letmein welcome welcome1 welcome123 admin admin123 administrator root toor changeme
default guest iloveyou monkey dragon football baseball master sunshine princess shadow superman michael jordan trustno1
whatever freedom secret login hello123 starwars passw0rd1 nopewarden firewall router netgear linksys cisco ubnt ubiquiti
opnsense pfsense mikrotik raspberry letmein1 test1234 testtest temp1234 summer2020 summer2021 winter2020 spring2020
iloveyou1 123123123 111111111 999999999 987654321 123321123 aaaaaaaa zaq12wsx pa55word pa55w0rd`) {
		m[w] = true
	}
	return m
}()
