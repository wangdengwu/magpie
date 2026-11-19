package teamd

import (
	"strings"
	"testing"
)

// TestConfigValidateNamesWhatIsMissing: every setting is checked at start,
// with the one that is wrong named, so a misconfigured sidecar says why.
func TestConfigValidateNamesWhatIsMissing(t *testing.T) {
	ok := Config{
		AdminURL:     DefaultAdminURL,
		AdminKeyFile: "/config/teamd/admin.key",
		Listen:       DefaultListen,
		StateDir:     "/config/teamd",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a complete config was refused: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Config)
		says   string
	}{
		{"no admin address", func(c *Config) { c.AdminURL = "" }, "-admin-url"},
		{"another machine", func(c *Config) { c.AdminURL = "http://192.0.2.10:3425" }, "not this computer"},
		{"no scheme", func(c *Config) { c.AdminURL = "127.0.0.1:3425" }, "http://"},
		{"a query", func(c *Config) { c.AdminURL = "http://127.0.0.1:3425/?k=x" }, "plain host"},
		{"no key file", func(c *Config) { c.AdminKeyFile = "  " }, "-admin-key-file"},
		{"listen without a port", func(c *Config) { c.Listen = "127.0.0.1" }, "host:port"},
		{"no state folder", func(c *Config) { c.StateDir = "" }, "-state-dir"},
	} {
		c := ok
		tc.change(&c)
		err := c.Validate()
		if err == nil {
			t.Errorf("%s was accepted", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s answered %q, which doesn't say %q", tc.name, err, tc.says)
		}
	}
}

// TestLoopbackBaseIsTheSameAddress: an address is taken as the same base
// URL however it is written, and only as this computer.
func TestLoopbackBaseIsTheSameAddress(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:3425", "http://127.0.0.1:3425/", " http://127.0.0.1:3425// "} {
		got, err := loopbackBase(raw)
		if err != nil {
			t.Errorf("%q was refused: %v", raw, err)
			continue
		}
		if got != "http://127.0.0.1:3425" {
			t.Errorf("%q read as %q", raw, got)
		}
	}
	for _, raw := range []string{"http://localhost:3425", "http://LOCALHOST:3425", "http://[::1]:3425", "https://127.0.0.1:3443"} {
		if _, err := loopbackBase(raw); err != nil {
			t.Errorf("%q was refused: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://0.0.0.0:3425", "http://192.0.2.10:3425", "http://magpie:3425", "http://127.0.0.1.nip.io:3425"} {
		if _, err := loopbackBase(raw); err == nil {
			t.Errorf("%q was accepted as this computer", raw)
		}
	}
}

// TestNewTokenIsRandomAndHashed: two tokens are never alike, and what is
// kept of one is not the token.
func TestNewTokenIsRandomAndHashed(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		token, hash, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(token) < 40 {
			t.Fatalf("a token is %d characters: %q", len(token), token)
		}
		if token == hash {
			t.Fatal("the hash is the token")
		}
		if hash != HashToken(token) {
			t.Fatalf("the hash kept isn't HashToken of the token")
		}
		if strings.ContainsAny(token, "+/= ") {
			t.Fatalf("the token %q isn't safe in a URL path", token)
		}
		if seen[token] {
			t.Fatalf("the token %q was made twice", token)
		}
		seen[token] = true
	}
}

// TestSameTokenComparesHashes: the boundary's comparison answers the same
// hash and no other.
func TestSameTokenComparesHashes(t *testing.T) {
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !sameToken(hash, HashToken(token)) {
		t.Error("a token's own hash didn't match")
	}
	if sameToken(hash, HashToken(token+"x")) {
		t.Error("another token's hash matched")
	}
	if sameToken("", "") {
		t.Error("two empty hashes matched: a team with no token would answer every empty one")
	}
	if sameToken(hash, "") {
		t.Error("an empty hash matched")
	}
}
