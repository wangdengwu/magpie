package teamd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newAdminClientFor is an AdminClient over f, with its key in a file.
func newAdminClientFor(t *testing.T, f *fakeMagpie, key string) *AdminClient {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.key")
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewAdminClient(Config{AdminURL: f.URL, AdminKeyFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestAdminClientSendsTheKey: every call carries the key as a bearer
// token, which is how magpie knows the caller may manage it.
func TestAdminClientSendsTheKey(t *testing.T) {
	f := newFakeMagpie(t, []Group{{ID: groupA, Accounts: []string{acctA1}}})
	c := newAdminClientFor(t, f, testAdminKey)
	groups, err := c.AccountGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != groupA {
		t.Fatalf("read %+v, want one group %q", groups, groupA)
	}
	if f.callsTo() != 1 {
		t.Errorf("magpie was called %d times, want 1", f.callsTo())
	}
}

// TestAdminKeyIsNotInAnError: a reply that repeats the key can't carry it
// into an error, which a page or a log line would then hold.
func TestAdminKeyIsNotInAnError(t *testing.T) {
	f := newFakeMagpie(t, nil)
	f.mu.Lock()
	f.echoKey = true
	f.mu.Unlock()
	c := newAdminClientFor(t, f, testAdminKey)
	_, err := c.AccountGroups(context.Background())
	if err == nil {
		t.Fatal("a failing magpie answered no error")
	}
	if strings.Contains(err.Error(), testAdminKey) {
		t.Fatalf("the error holds the admin key: %v", err)
	}
	if !strings.Contains(err.Error(), "[admin key]") {
		t.Errorf("the echoed key isn't named as redacted: %v", err)
	}
	// nor does the client, printed, say it
	if s := fmt.Sprintf("%v %s", c, c); strings.Contains(s, testAdminKey) {
		t.Errorf("the client prints the admin key: %s", s)
	}
}

// TestAdminKeyFileIsReadAsOneKey: a file holding more than one line, or
// nothing, is refused at start rather than guessed at.
func TestAdminKeyFileIsReadAsOneKey(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"empty":      "",
		"blank line": "\n\n",
		"two keys":   "sk-magpie-key-one\nsk-magpie-key-two\n",
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewAdminClient(Config{AdminURL: DefaultAdminURL, AdminKeyFile: path}); err == nil {
			t.Errorf("the admin key file %q was accepted", name)
		}
	}
	// the file named that isn't there is refused, not taken as no key
	if _, err := NewAdminClient(Config{AdminURL: DefaultAdminURL, AdminKeyFile: filepath.Join(dir, "absent")}); err == nil {
		t.Error("a missing admin key file was accepted")
	}
}

// TestAdminClientRefusesAnotherMachine: the key is AdminLocal, so a
// magpie anywhere but loopback is refused at start.
func TestAdminClientRefusesAnotherMachine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.key")
	if err := os.WriteFile(path, []byte(testAdminKey), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"http://192.0.2.10:3425", "https://gateway.example.com", "http://example.com:3425", "ftp://127.0.0.1:3425", "127.0.0.1:3425"} {
		if _, err := NewAdminClient(Config{AdminURL: url, AdminKeyFile: path}); err == nil {
			t.Errorf("magpie at %q was accepted", url)
		}
	}
	// loopback, by address or name, is
	for _, url := range []string{"http://127.0.0.1:3425", "http://localhost:3425/", "http://[::1]:3425"} {
		if _, err := NewAdminClient(Config{AdminURL: url, AdminKeyFile: path}); err != nil {
			t.Errorf("magpie at %q was refused: %v", url, err)
		}
	}
}

// TestAdminClientFailsOnAnAnswerThatDoesntRead: a body that isn't the
// group list is an error, so the boundary refuses rather than reading it
// as no groups.
func TestAdminClientFailsOnAnAnswerThatDoesntRead(t *testing.T) {
	f := newFakeMagpie(t, nil)
	f.mu.Lock()
	f.status, f.body = 200, "not json at all"
	f.mu.Unlock()
	c := newAdminClientFor(t, f, testAdminKey)
	if _, err := c.AccountGroups(context.Background()); err == nil {
		t.Fatal("an answer that doesn't parse read as groups")
	}
	f.mu.Lock()
	f.status, f.body = 200, ""
	f.mu.Unlock()
	if _, err := c.AccountGroups(context.Background()); err == nil {
		t.Fatal("an empty answer read as groups")
	}
}

// TestAdminClientRefusesAKeyMagpieDoesNotKnow: magpie's 401 comes back as
// an error naming the status, and the caller does not go on.
func TestAdminClientRefusesAKeyMagpieDoesNotKnow(t *testing.T) {
	f := newFakeMagpie(t, nil)
	c := newAdminClientFor(t, f, "sk-magpie-key-not-a-real-one")
	_, err := c.AccountGroups(context.Background())
	if err == nil {
		t.Fatal("magpie's 401 answered no error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("the error doesn't name magpie's answer: %v", err)
	}
	if errors.Is(err, ErrNoTeam) || errors.Is(err, ErrNotYours) {
		t.Errorf("a failed call was answered as a refusal: %v", err)
	}
}
