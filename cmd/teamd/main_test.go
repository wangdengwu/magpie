package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/teamd"
	"github.com/yetone/magpie/internal/testenv"
)

// testKey is a key shaped like an admin one and used as no key at all:
// nothing here starts a magpie to answer with it.
const testKey = "sk-magpie-key-0000000000000000000000000000000000000000000000000000000000000000"

func TestMain(m *testing.M) { testenv.Main(m) }

// runIn runs teamd with args and gives what it exited with and printed. No
// test here starts a listener or reaches magpie: a command that needs
// magpie is given an address nothing answers, so what it says about the
// arguments is what the test reads.
func runIn(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	code = run(args, &out, &errs)
	return code, out.String(), errs.String()
}

// stateWith is a state folder holding one team, and the flags that point
// at it, at an admin address nothing answers.
func stateWith(t *testing.T, teams ...teamd.Team) []string {
	t.Helper()
	dir := t.TempDir()
	keyFile := dir + "/admin.key"
	if err := os.WriteFile(keyFile, []byte(testKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := teamd.NewStore(dir+"/state", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) > 0 {
		if err := store.Save(teams); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"-admin-url", "http://127.0.0.1:1", "-admin-key-file", keyFile, "-state-dir", dir + "/state"}
}

// TestCommandsTakeTheirWordBeforeTheirFlags: every command documented as
// `teamd teams token <team> --flag` reads the team as the team, not as a
// flag's value or a stray argument.
func TestCommandsTakeTheirWordBeforeTheirFlags(t *testing.T) {
	flags := stateWith(t, teamd.Team{ID: "team-a", Name: "Team A", GroupID: "6f1c2a9d4b70", KeyID: "aaaa1111bbbb2222cccc3333"})

	// token reads no magpie at all, so it runs against an address nothing
	// answers and still issues a link
	code, stdout, stderr := runIn(t, append([]string{"teams", "token", "team-a"}, flags...)...)
	if code != 0 {
		t.Fatalf("teams token team-a exited %d: %s", code, stderr)
	}
	token := strings.TrimSpace(stdout)
	if len(token) < 40 || strings.ContainsAny(token, " \t") {
		t.Fatalf("stdout is %q, want the token alone", stdout)
	}
	if !strings.Contains(stderr, "only time it is printed") {
		t.Errorf("stderr doesn't say the token is shown once: %q", stderr)
	}

	// rm parses its team too: what fails is magpie, which nothing answers,
	// and not the argument
	code, _, stderr = runIn(t, append([]string{"teams", "rm", "team-a"}, flags...)...)
	if code != 1 {
		t.Fatalf("teams rm team-a exited %d, want 1: %s", code, stderr)
	}
	if strings.Contains(stderr, "takes one team") {
		t.Fatalf("teams rm didn't read \"team-a\" as the team: %s", stderr)
	}
	if !strings.Contains(stderr, "can't be read") {
		t.Errorf("the failure isn't magpie's: %q", stderr)
	}

	// and add, whose name comes first and whose flags come after it
	code, _, stderr = runIn(t, append([]string{"teams", "add", "Team B", "--group", "b3d8e04c17a2", "--key", "dddd4444eeee5555ffff6666"}, flags...)...)
	if code != 1 {
		t.Fatalf("teams add exited %d, want 1: %s", code, stderr)
	}
	if strings.Contains(stderr, "takes one name") || strings.Contains(stderr, "needs --group and --key") {
		t.Fatalf("teams add didn't read its name and flags: %s", stderr)
	}
}

// TestAddSaysWhatItNeeds: the flags are named when one is left out, before
// anything is opened.
func TestAddSaysWhatItNeeds(t *testing.T) {
	flags := stateWith(t)
	code, _, stderr := runIn(t, append([]string{"teams", "add", "Team A", "--group", "6f1c2a9d4b70"}, flags...)...)
	if code != 2 {
		t.Fatalf("teams add without --key exited %d, want 2: %s", code, stderr)
	}
	if !strings.Contains(stderr, "--group and --key") {
		t.Errorf("the message doesn't name what is missing: %q", stderr)
	}
}

// TestServeRefusesWithoutAnAdminKey: a sidecar started without its key
// says which file it wanted, and starts nothing.
func TestServeRefusesWithoutAnAdminKey(t *testing.T) {
	code, _, stderr := runIn(t, "serve", "-state-dir", t.TempDir(), "-listen", "127.0.0.1:0")
	if code != 1 {
		t.Fatalf("serve without a key exited %d, want 1: %s", code, stderr)
	}
	if !strings.Contains(stderr, "-admin-key-file") {
		t.Errorf("the message doesn't name the flag: %q", stderr)
	}
}

// TestHelpIsNotAnError: help is what was asked for.
func TestHelpIsNotAnError(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"serve", "-h"}, {"teams", "add", "-h"}} {
		if code, _, _ := runIn(t, args...); code != 0 {
			t.Errorf("%v exited %d, want 0", args, code)
		}
	}
	if code, _, stderr := runIn(t, "tokens"); code != 2 || !strings.Contains(stderr, "not a command") {
		t.Errorf("an unknown command exited %d: %q", code, stderr)
	}
}

// teamsIn reads back the teams a state folder's flags point at.
func teamsIn(t *testing.T, flags []string) []teamd.Team {
	t.Helper()
	store, err := teamd.NewStore(flags[len(flags)-1], 0)
	if err != nil {
		t.Fatal(err)
	}
	teams, err := store.Teams()
	if err != nil {
		t.Fatal(err)
	}
	return teams
}

// TestTokenTakesALifetime: --ttl gives the link an end, no --ttl gives it
// none, and a lifetime it doesn't know is refused before anything is
// written.
func TestTokenTakesALifetime(t *testing.T) {
	team := teamd.Team{ID: "team-a", Name: "Team A", GroupID: "6f1c2a9d4b70", KeyID: "aaaa1111bbbb2222cccc3333"}

	flags := stateWith(t, team)
	before := time.Now()
	if code, _, stderr := runIn(t, append([]string{"teams", "token", "team-a", "--ttl", "30d"}, flags...)...); code != 0 {
		t.Fatalf("teams token --ttl 30d exited %d: %s", code, stderr)
	}
	ends := teamsIn(t, flags)[0].ExpiresAt
	if lo, hi := before.Add(30*24*time.Hour), time.Now().Add(30*24*time.Hour); ends.Before(lo) || ends.After(hi) {
		t.Errorf("the link ends %v, want 30 days from the command, between %v and %v", ends, lo, hi)
	}

	flags = stateWith(t, team)
	if code, _, stderr := runIn(t, append([]string{"teams", "token", "team-a"}, flags...)...); code != 0 {
		t.Fatalf("teams token exited %d: %s", code, stderr)
	}
	if ends := teamsIn(t, flags)[0].ExpiresAt; !ends.IsZero() {
		t.Errorf("a link issued with no --ttl ends %v, want never", ends)
	}

	flags = stateWith(t, team)
	code, stdout, stderr := runIn(t, append([]string{"teams", "token", "team-a", "--ttl", "nonsense"}, flags...)...)
	if code != 2 {
		t.Fatalf("teams token --ttl nonsense exited %d, want 2: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("a refused lifetime printed a token: %q", stdout)
	}
	if !strings.Contains(stderr, "7d, 30d, 90d or forever") {
		t.Errorf("the refusal doesn't name the lifetimes there are: %q", stderr)
	}
	if got := teamsIn(t, flags)[0]; got.TokenHash != "" || !got.ExpiresAt.IsZero() {
		t.Errorf("a refused lifetime wrote the team: %+v", got)
	}
}

// TestListSaysWhenEachLinkEnds: the TOKEN column tells a link with no end
// from one with an end, one past it, and no link at all.
func TestListSaysWhenEachLinkEnds(t *testing.T) {
	soon := time.Now().Add(48 * time.Hour)
	flags := stateWith(t,
		teamd.Team{ID: "team-a", Name: "A", GroupID: "g1", KeyID: "k1", TokenHash: "h1"},
		teamd.Team{ID: "team-b", Name: "B", GroupID: "g2", KeyID: "k2", TokenHash: "h2", ExpiresAt: soon},
		teamd.Team{ID: "team-c", Name: "C", GroupID: "g3", KeyID: "k3", TokenHash: "h3", ExpiresAt: time.Now().Add(-time.Hour)},
		teamd.Team{ID: "team-d", Name: "D", GroupID: "g4", KeyID: "k4"},
	)
	code, stdout, stderr := runIn(t, append([]string{"teams", "list"}, flags...)...)
	if code != 0 {
		t.Fatalf("teams list exited %d: %s", code, stderr)
	}
	for id, want := range map[string]string{
		"team-a": " live ",
		"team-b": " live until " + soon.Local().Format("2006-01-02 15:04") + " ",
		"team-c": " expired ",
		"team-d": " none ",
	} {
		line := ""
		for _, l := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(l, id+" ") {
				line = l
			}
		}
		if !strings.Contains(line, want) {
			t.Errorf("%s's line is %q, want it to say %q", id, line, want)
		}
	}
}
