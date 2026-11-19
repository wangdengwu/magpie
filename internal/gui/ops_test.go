package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/teamd"
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
	if rec := opsDo(t, h, "POST", "/ops/teams/add", url.Values{
		"name": {"team-a"}, "group": {g.ID}, "key": {keyID},
	}); rec.Code != http.StatusOK {
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
