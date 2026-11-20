package gui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/teamcore"
)

// groupForTest and outForTest stand in for the page's own ids, which it
// makes: a case says what a key names against these, and the test puts the
// real ones in.
const (
	groupForTest = "<the group the page made>"
	outForTest   = "<a group the key keeps out>"
)

// opsPage opens the page's own state folder and its reading of magpie,
// with no key and no container: what magpie's page does.
func opsPage(t *testing.T) http.Handler {
	t.Helper()
	sandboxHome(t)
	dir := filepath.Join(t.TempDir(), "teamd")
	store, err := teamcore.NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	return opsRoutes(store, guiMagpie{})
}

// opsDo sends the page's request: a GET, or a POST of body as JSON from
// the page itself.
func opsDo(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b := ""
	if body != nil {
		j, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		b = string(j)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(b))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	h.ServeHTTP(rec, r)
	return rec
}

// opsListed is the page's listing.
func opsListed(t *testing.T, h http.Handler) opsList {
	t.Helper()
	rec := opsDo(t, h, "GET", "/ops/teams/list", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the listing answered %d: %s", rec.Code, rec.Body)
	}
	var out opsList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

// opsAdd makes a team from the page and answers what the page was told.
func opsAdd(t *testing.T, h http.Handler, name, group, key string) opsLink {
	t.Helper()
	rec := opsDo(t, h, "POST", "/ops/teams/add", opsAsk{Name: name, Group: group, Key: key})
	if rec.Code != http.StatusOK {
		t.Fatalf("adding answered %d: %s", rec.Code, rec.Body)
	}
	var out opsLink
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

// opsGroupAndKey makes an account group and a gateway key in this magpie,
// as ops does on the Gateway page before making a team of them.
func opsGroupAndKey(t *testing.T) (access.AccountGroup, string) {
	t.Helper()
	g, err := access.SaveAccountGroup(access.AccountGroup{Name: "team-a"})
	if err != nil {
		t.Fatal(err)
	}
	if secret, err := access.Update("add-key", access.Change{Name: "team-a-key"}); err != nil || secret == "" {
		t.Fatal(err)
	}
	keys, err := access.List()
	if err != nil {
		t.Fatal(err)
	}
	return g, keys[len(keys)-1].ID
}

// keyAccounts are what key id names now.
func keyAccounts(t *testing.T, id string) []string {
	t.Helper()
	keys, err := access.List()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(keys, func(k access.Key) bool { return k.ID == id })
	if i < 0 {
		t.Fatalf("there is no key %s", id)
	}
	return keys[i].Accounts
}

// TestOpsPageAddsATeamAndGivesItsLink: the page lists magpie's own groups
// and keys to make a team of, and the link is told once: the listing after
// it says a link is out, and never what it is.
func TestOpsPageAddsATeamAndGivesItsLink(t *testing.T) {
	h := opsPage(t)
	g, keyID := opsGroupAndKey(t)

	before := opsListed(t, h)
	if !slices.ContainsFunc(before.Groups, func(o opsGroup) bool { return o.ID == g.ID && o.Name == "team-a" }) {
		t.Errorf("the page doesn't offer the group: %+v", before.Groups)
	}
	if !slices.ContainsFunc(before.Keys, func(o opsKey) bool { return o.ID == keyID }) {
		t.Errorf("the page doesn't offer the key: %+v", before.Keys)
	}

	made := opsAdd(t, h, "team-a", g.ID, keyID)
	token, ok := strings.CutPrefix(made.Link, "/team/")
	if !ok || token == "" {
		t.Fatalf("the answer carries no link: %+v", made)
	}

	rec := opsDo(t, h, "GET", "/ops/teams/list", nil)
	if strings.Contains(rec.Body.String(), token) {
		t.Fatalf("the listing repeats the token: %s", rec.Body)
	}
	after := opsListed(t, h)
	if len(after.Teams) != 1 {
		t.Fatalf("the listing has %d teams, want 1: %+v", len(after.Teams), after.Teams)
	}
	team := after.Teams[0]
	if team.ID != "team-a" || team.GroupName != "team-a" || team.KeyName != "team-a-key" || !team.Linked {
		t.Errorf("the team is listed as %+v", team)
	}
	if team.Held == nil || !*team.Held {
		t.Errorf("the team's key isn't listed as held to its group: %+v", team)
	}
	if team.Accounts == nil || *team.Accounts != 0 {
		t.Errorf("the team's group is listed with %v accounts, want 0", team.Accounts)
	}
}

// TestOpsPageRefusesWithItsReason: a team the page can't make is an error
// the page shows, and no team.
func TestOpsPageRefusesWithItsReason(t *testing.T) {
	h := opsPage(t)
	_, keyID := opsGroupAndKey(t)
	rec := opsDo(t, h, "POST", "/ops/teams/add", opsAsk{Name: "team-a", Group: "000000000000", Key: keyID})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("an add on a group magpie hasn't answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(opsListed(t, h).Teams); n != 0 {
		t.Errorf("a refused add made %d teams", n)
	}
}

// TestOpsPageRemovesATeam: a team removed leaves the listing; its group and
// key stay in magpie as they were.
func TestOpsPageRemovesATeam(t *testing.T) {
	h := opsPage(t)
	g, keyID := opsGroupAndKey(t)
	opsAdd(t, h, "team-a", g.ID, keyID)
	rec := opsDo(t, h, "POST", "/ops/teams/remove", opsAsk{ID: "team-a"})
	if rec.Code != http.StatusOK {
		t.Fatalf("removing answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(opsListed(t, h).Teams); n != 0 {
		t.Fatalf("the team is still listed")
	}
	if want := []string{"@" + g.ID}; !slices.Equal(keyAccounts(t, keyID), want) {
		t.Errorf("removing the team changed its key: %v, want %v", keyAccounts(t, keyID), want)
	}
}

// TestOpsPageHoldsAnOldTeamsKey: a team made before the page held keys may
// have one that reaches every account magpie has. The listing says so, and
// Hold puts the group on the key, from the page.
func TestOpsPageHoldsAnOldTeamsKey(t *testing.T) {
	h := opsPage(t)
	g, keyID := opsGroupAndKey(t)
	opsAdd(t, h, "team-a", g.ID, keyID)
	// as an old team's key is: every account
	if _, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: nil}); err != nil {
		t.Fatal(err)
	}
	if team := opsListed(t, h).Teams[0]; team.Held == nil || *team.Held {
		t.Fatalf("a key that reaches every account is listed as held: %+v", team)
	}
	if rec := opsDo(t, h, "POST", "/ops/teams/hold", opsAsk{ID: "team-a"}); rec.Code != http.StatusOK {
		t.Fatalf("holding answered %d: %s", rec.Code, rec.Body)
	}
	if want := []string{"@" + g.ID}; !slices.Equal(keyAccounts(t, keyID), want) {
		t.Errorf("the key's accounts are %v, want %v", keyAccounts(t, keyID), want)
	}
	if team := opsListed(t, h).Teams[0]; team.Held == nil || !*team.Held {
		t.Errorf("the held key isn't listed as held: %+v", team)
	}
}

// TestOpsPageHoldsTheKeyToItsGroup: a key that names no account reaches
// every account magpie has, so the page holds it to the team's group as it
// makes the team. A key already held to the group is left alone, extra
// accounts and all: what it names is what that team's ops meant it to
// reach.
//
// Ops reads magpie's keys through its own conversion (guiMagpie.GatewayKeys),
// and the "held already" case is what says that conversion carries the
// accounts: a field dropped there reads as no key being held, so the page
// would write accounts-key over a key that was already right and take the
// accounts away.
func TestOpsPageHoldsTheKeyToItsGroup(t *testing.T) {
	for _, tc := range []struct {
		name string
		held []string // what the key names before the add
		want []string // and after it
	}{
		{"a key held to nothing", nil, []string{"@" + groupForTest}},
		{"a key held to its group", []string{"@" + groupForTest}, []string{"@" + groupForTest}},
		// a second entry the page must not take away. It is a group entry
		// because CleanAccounts checks every provider one against the
		// providers this magpie has, which a test's page has none of.
		{"a key naming a group kept out too", []string{"@" + groupForTest, "!@" + outForTest},
			[]string{"@" + groupForTest, "!@" + outForTest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := opsPage(t)
			g, err := access.SaveAccountGroup(access.AccountGroup{Name: "team-a"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := access.Update("add-key", access.Change{Name: "team-a-key"}); err != nil {
				t.Fatal(err)
			}
			keys, err := access.List()
			if err != nil {
				t.Fatal(err)
			}
			keyID := keys[len(keys)-1].ID
			before := tc.held
			for i := range before {
				before[i] = strings.ReplaceAll(before[i], groupForTest, g.ID)
			}
			if len(before) > 1 {
				// a group of its own for the second entry, so the two are
				// told apart as the test means them
				other, err := access.SaveAccountGroup(access.AccountGroup{Name: "elsewhere"})
				if err != nil {
					t.Fatal(err)
				}
				for i := range before {
					before[i] = strings.ReplaceAll(before[i], outForTest, other.ID)
				}
				for i := range tc.want {
					tc.want[i] = strings.ReplaceAll(tc.want[i], outForTest, other.ID)
				}
			}
			if len(before) > 0 {
				if _, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: before}); err != nil {
					t.Fatal(err)
				}
			}

			if made := opsAdd(t, h, "team-a", g.ID, keyID); !strings.HasPrefix(made.Link, "/team/") {
				t.Fatalf("the add answered no link: %+v", made)
			}
			after, err := access.List()
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(after, func(k access.Key) bool { return k.ID == keyID })
			if i < 0 {
				t.Fatal("the key is gone")
			}
			want := tc.want
			for j := range want {
				want[j] = strings.ReplaceAll(want[j], groupForTest, g.ID)
			}
			if !slices.Equal(after[i].Accounts, want) {
				t.Errorf("the key's accounts are %v, want %v", after[i].Accounts, want)
			}
		})
	}
}

// TestOpsPageNeedsThePageItself: a request from another site's page is
// turned away, as every write of the app's is.
func TestOpsPageNeedsThePageItself(t *testing.T) {
	h := opsPage(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/ops/teams/add", strings.NewReader(`{"name":"x","group":"y","key":"z"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("another site's page answered %d, want 403", rec.Code)
	}
}

// TestOnlyAPortalHasTheTeamPortalPage: a magpie with no portal beside it
// (no MAGPIE_TEAMD_DIR) serves no /ops/ routes, makes no teamd folder, and
// its boot.js says nothing of a portal, so the page draws no entry for it.
// With one, boot.js says so and the routes answer.
func TestOnlyAPortalHasTheTeamPortalPage(t *testing.T) {
	ask := func(h http.Handler, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	t.Setenv("MAGPIE_TEAMD_DIR", "")
	h := builtinHome(t)
	if b := ask(h, "/boot.js").Body.String(); strings.Contains(b, "teamPortal") {
		t.Errorf("a magpie with no portal says it has one: %s", b)
	}
	if rec := ask(h, "/ops/teams/list"); strings.Contains(rec.Body.String(), `"teams"`) {
		t.Errorf("a magpie with no portal answers the page's routes: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(teamdOpsDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a magpie with no portal made %s: %v", teamdOpsDir(), err)
	}

	t.Setenv("MAGPIE_TEAMD_DIR", filepath.Join(t.TempDir(), "teamd"))
	h = builtinHome(t)
	if b := ask(h, "/boot.js").Body.String(); !strings.Contains(b, `"teamPortal":true`) {
		t.Errorf("a magpie beside a portal doesn't say so: %s", b)
	}
	if rec := ask(h, "/ops/teams/list"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"teams"`) {
		t.Errorf("the page's routes answered %d: %s", rec.Code, rec.Body)
	}
}
