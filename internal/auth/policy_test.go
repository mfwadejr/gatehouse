package auth

import (
	"strings"
	"testing"
)

func TestPolicyDefaults(t *testing.T) {
	p := DefaultPolicy()
	if p.Min != 8 || !p.Common || p.Upper || p.Lower || p.Digit || p.Symbol {
		t.Fatalf("defaults wrong: %+v", p)
	}
	if err := p.Check("sevench", "admin"); err == nil || !strings.Contains(err.Error(), "7 of the 8") {
		t.Fatalf("seven characters should be refused naming the count, got %v", err)
	}
	if err := p.Check("eightchr", "admin"); err != nil {
		t.Fatalf("eight characters should pass by default: %v", err)
	}
}

func TestPolicyTypesNamedInErrors(t *testing.T) {
	p := Policy{Min: 10, Upper: true, Lower: true, Digit: true, Symbol: true}
	cases := map[string]string{
		"lowercase1!":  "capital",
		"UPPERCASE1!":  "small letter",
		"NoNumbers!!!": "number",
		"NoSymbols123": "symbol",
		"Sh0rt!":       "too short",
	}
	for pw, want := range cases {
		err := p.Check(pw, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want error naming %q, got %v", pw, want, err)
		}
	}
	if err := p.Check("Goodpass1!x", ""); err != nil {
		t.Fatalf("a password meeting every rule should pass: %v", err)
	}
}

func TestCommonPasswordsRefused(t *testing.T) {
	p := DefaultPolicy()
	for _, pw := range []string{"password", "Password1!", "12345678", "qwerty123", "ADMIN123"} {
		if err := p.Check(pw, ""); err == nil {
			t.Errorf("%q should be refused as very common", pw)
		}
	}
	p.Common = false
	if err := p.Check("password", ""); err != nil {
		t.Errorf("with the switch off a common password passes: %v", err)
	}
	if err := DefaultPolicy().Check("correct horse battery", ""); err != nil {
		t.Errorf("a passphrase passes: %v", err)
	}
}

func TestPolicyClampedAndStored(t *testing.T) {
	if (Policy{Min: 3}).Clean().Min != 8 || (Policy{Min: 500}).Clean().Min != 64 {
		t.Fatal("the shortest password stays between 8 and 64")
	}
}

func TestStoredPolicyAppliesToNewPasswordsOnly(t *testing.T) {
	s, _ := newSvc(t)
	if _, err := s.CreateUser("admin", "eightchr", "admin"); err != nil {
		t.Fatalf("eight characters are fine on a new box: %v", err)
	}
	u, _ := s.UserByName("admin")
	if err := s.SetPolicy(Policy{Min: 14, Symbol: true, Common: true}); err != nil {
		t.Fatal(err)
	}
	if got := s.Policy(); got.Min != 14 || !got.Symbol || got.Upper {
		t.Fatalf("stored policy not read back: %+v", got)
	}
	// The password already set still signs in; nothing is locked out by a raised minimum.
	if err := s.CheckPassword("admin", "eightchr", "10.0.0.1"); err != nil {
		t.Fatalf("an existing password must keep working after the rules are raised: %v", err)
	}
	// A new one has to meet the new rules, and the error names the rule.
	if err := s.SetPassword(u.ID, "shortone"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("new password should be held to the raised minimum, got %v", err)
	}
	if err := s.SetPassword(u.ID, "long enough words here!"); err != nil {
		t.Fatalf("a password meeting the new rules is accepted: %v", err)
	}
	if err := s.CheckNewPassword("fourteencharsx", ""); err == nil || !strings.Contains(err.Error(), "symbol") {
		t.Fatalf("backup and offsite passwords use the same check, got %v", err)
	}
}
