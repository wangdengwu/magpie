package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/teamd"
)

// groupForTest and acctForTest stand in for the page's own ids, which it
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
	store, err := teamd.NewStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	return opsRoutes(store, guiMagpie{})
}

func opsDo(t *testing.T, h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://"+r.Host)
	}
	h.ServeHTTP(rec, r)
	return rec
}

// TestOpsPageAddsATeamAndGivesItsLink: the page ops makes teams with lists
// magpie's own groups and keys, and a link is shown once.
func TestOpsPageAddsATeamAndGivesItsLink(t *testing.T) {
	h := opsPage(t)
	g, err := access.SaveAccountGroup(access.AccountGroup{Name: "team-a"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := access.Update("add-key", access.Change{Name: "team-a-key"})
	if err != nil || secret == "" {
		t.Fatal(err)
	}
	keys, err := access.List()
	if err != nil {
		t.Fatal(err)
	}
	keyID := keys[len(keys)-1].ID
	// the key names the group, as a team's key must (teamd's Ops.Add): one
	// that names no account reaches every account this magpie has
	if _, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: []string{"@" + g.ID}}); err != nil {
		t.Fatal(err)
	}

	page := opsDo(t, h, "GET", "/ops/teams", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the page answered %d: %s", page.Code, page.Body.String())
	}
	for _, want := range []string{"team-a", keyID} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the page doesn't offer %q:\n%s", want, page.Body.String())
		}
	}

	rec := opsDo(t, h, "POST", "/ops/teams/add", url.Values{
		"name": {"team-a"}, "group": {g.ID}, "key": {keyID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("adding answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if i := strings.Index(body, `class="bad"`); i >= 0 {
		t.Fatalf("the add was refused: %s", body[i:i+200])
	}
	if !strings.Contains(body, "/team/") {
		t.Fatalf("the answer carries no link:\n%s", body)
	}
	token := body[strings.Index(body, "/team/")+len("/team/"):]
	token = token[:strings.IndexAny(token, "\"< ")]

	// the team is on the page, with its link live
	list := opsDo(t, h, "GET", "/ops/teams", nil).Body.String()
	if !strings.Contains(list, "team-a") || strings.Contains(list, token) {
		t.Fatalf("the listing after the add is wrong, or repeats the token:\n%s", list)
	}
}

// TestOpsPageRemovesATeam: the form removes one, and says when magpie's
// group still holds accounts.
func TestOpsPageRemovesATeam(t *testing.T) {
	h := opsPage(t)
	g, err := access.SaveAccountGroup(access.AccountGroup{Name: "team-a"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := access.Update("add-key", access.Change{Name: "team-a-key"})
	if err != nil || secret == "" {
		t.Fatal(err)
	}
	keys, _ := access.List()
	keyID := keys[len(keys)-1].ID
	if _, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: []string{"@" + g.ID}}); err != nil {
		t.Fatal(err)
	}
	// the page answers 200 for a refused add too, with the reason drawn, so
	// the answer is read for it: a code check alone passes an add that made
	// no team
	if rec := opsDo(t, h, "POST", "/ops/teams/add", url.Values{
		"name": {"team-a"}, "group": {g.ID}, "key": {keyID},
	}); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `class="bad"`) {
		t.Fatalf("adding answered %d: %s", rec.Code, rec.Body.String())
	}
	rec := opsDo(t, h, "POST", "/ops/teams/remove", url.Values{"id": {"team-a"}, "force": {"1"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("removing answered %d: %s", rec.Code, rec.Body.String())
	}
	if list := opsDo(t, h, "GET", "/ops/teams", nil).Body.String(); strings.Contains(list, "<td>team-a</td>") {
		t.Fatalf("the team is still listed:\n%s", list)
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

			rec := opsDo(t, h, "POST", "/ops/teams/add", url.Values{
				"name": {"team-a"}, "group": {g.ID}, "key": {keyID},
			})
			body := rec.Body.String()
			if strings.Contains(body, `class="bad"`) {
				t.Fatalf("the page refused the add:\n%s", body)
			}
			if !strings.Contains(body, "/team/") {
				t.Fatalf("the answer carries no link:\n%s", body)
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
	r := httptest.NewRequest("POST", "/ops/teams/add", strings.NewReader("name=x&group=y&key=z"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://elsewhere.example")
	h.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatalf("another site's page added a team: %d", rec.Code)
	}
}
