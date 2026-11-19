package teamd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// get asks the portal for a path the way a browser would, and gives the
// answer.
func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestAccountsPageShowsTheTeamsOwnAccounts is the page's whole job: the
// accounts its group holds, with their windows, and nothing of another
// team's.
func TestAccountsPageShowsTheTeamsOwnAccounts(t *testing.T) {
	f := newFixture(t)
	rec := get(t, f.srv, "/team/"+f.tokenA)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /team/<token> answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Team A",            // the team's name
		"alice@example.com", // an account its group holds
		"bob@example.com",   // and the other
		"Max",               // a plan
		"5 hours", "7 days", // both windows
		"42%", "100%", // what they used
		"in use",                 // alice is
		"2 of 2 accounts in use", // the count
		"6f1c2a9d4b70",           // the group's id, for ops to check
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't say %q:\n%s", want, body)
		}
	}
	// another team's account is in the same magpie answers, and must not
	// reach this page
	for _, leak := range []string{"carol@example.com", acctB1, groupB, "Team B", keyB} {
		if strings.Contains(body, leak) {
			t.Errorf("the page names %q, which is team B's:\n%s", leak, body)
		}
	}
}

// TestAccountsPageSaysWhenAWindowIsUnknown: an account magpie has no
// reading of says so, and is never drawn as an account at 0%.
func TestAccountsPageSaysWhenAWindowIsUnknown(t *testing.T) {
	f := newFixture(t)
	// carol is in B's group and has no reading; put her in A's for a look
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, acctB1}},
		{ID: groupB, Name: "Team B", Accounts: []string{}},
	})
	rec := get(t, f.srv, "/team/"+f.tokenA)
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "carol@example.com") {
		t.Fatalf("the page doesn't show the account that was added to its group:\n%s", body)
	}
	if !strings.Contains(body, "magpie has no reading") {
		t.Errorf("the page doesn't say why carol has no windows:\n%s", body)
	}
	// and it is not drawn as a zero
	if strings.Contains(body, ">0%<") {
		t.Errorf("an account magpie never read is drawn at 0%%:\n%s", body)
	}
}

// TestAccountsPageShowsAnEntryMagpieLost: a group naming an account signed
// out since keeps it, and the page says it matches nothing.
func TestAccountsPageShowsAnEntryMagpieLost(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, "claude/ffffffffffffffff"}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	rec := get(t, f.srv, "/team/"+f.tokenA)
	body := rec.Body.String()
	if !strings.Contains(body, "claude/ffffffffffffffff") {
		t.Errorf("the page drops an entry its group holds:\n%s", body)
	}
	if !strings.Contains(body, "no account magpie has now") {
		t.Errorf("the page doesn't say the entry matches nothing:\n%s", body)
	}
}

// TestAccountsPageWithAnEmptyGroup: an empty pool is said, not drawn as an
// empty list.
func TestAccountsPageWithAnEmptyGroup(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A"},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	rec := get(t, f.srv, "/team/"+f.tokenA)
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty group answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pool is empty") {
		t.Errorf("the page doesn't say the pool is empty:\n%s", rec.Body.String())
	}
}

// TestAccountsPageRefusesAnUnknownToken: a link naming no team is 404, and
// says nothing about why.
func TestAccountsPageRefusesAnUnknownToken(t *testing.T) {
	f := newFixture(t)
	for _, token := range []string{"nope", f.tokenA[:len(f.tokenA)-1], strings.Repeat("a", 43)} {
		rec := get(t, f.srv, "/team/"+token)
		if rec.Code != http.StatusNotFound {
			t.Errorf("/team/%s answered %d, want 404", token, rec.Code)
		}
		for _, leak := range []string{"team-a", "Team A", groupA, "alice"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("a 404 names %q: %s", leak, rec.Body.String())
			}
		}
	}
}

// TestAccountsPageIsNotCacheableAndLeaksNoReferrer: the token is in the
// path, so nothing here may be kept or passed on.
func TestAccountsPageIsNotCacheableAndLeaksNoReferrer(t *testing.T) {
	f := newFixture(t)
	rec := get(t, f.srv, "/team/"+f.tokenA)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy is %q, want no-referrer", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the page's CSP is %q, which doesn't say %q", csp, want)
		}
	}
	// no page loads anything from another origin
	body := rec.Body.String()
	for _, bad := range []string{"http://", "https://", "<script", "src="} {
		if strings.Contains(body, bad) {
			t.Errorf("the page holds %q, which would reach another origin:\n%s", bad, body)
		}
	}
}

// TestAccountsPageSaysWhenMagpieCantBeRead: an unreadable magpie is 503 and
// says nothing magpie said, which can name another team's accounts.
func TestAccountsPageSaysWhenMagpieCantBeRead(t *testing.T) {
	f := newFixture(t)
	f.magpie.fail(500, `{"error":"providers.json can't be read: claude/carol@example.com"}`)
	rec := get(t, f.srv, "/team/"+f.tokenA)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable magpie answered %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "carol@example.com") {
		t.Errorf("magpie's own error reached the page: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "didn't answer") {
		t.Errorf("the page doesn't say magpie didn't answer: %s", rec.Body.String())
	}
}

// TestAccountsPageEscapesWhatMagpieSays: an account name is data, and a
// name with markup in it is shown as text.
func TestAccountsPageEscapesWhatMagpieSays(t *testing.T) {
	f := newFixture(t)
	setModelName(t, f, acctA1, `<img src=x onerror=alert(1)>`)
	rec := get(t, f.srv, "/team/"+f.tokenA)
	body := rec.Body.String()
	if strings.Contains(body, "<img src=x") {
		t.Errorf("an account name put markup in the page:\n%s", body)
	}
	if !strings.Contains(body, "&lt;img") {
		t.Errorf("the name isn't shown as text:\n%s", body)
	}
}

// TestAccountsPageSaysWhichAccountsAreDisabled: a disabled account is drawn
// disabled, is not counted in use, and sorts after the ones that can serve.
func TestAccountsPageSaysWhichAccountsAreDisabled(t *testing.T) {
	f := newFixture(t)
	f.magpie.mu.Lock()
	// bob is the group's, and magpie was told to leave him alone
	f.magpie.providers[0]["account"].(map[string]any)["logins"] = []map[string]any{
		{"agent": "claude", "user": "alice@example.com", "plan": "Max", "active": true, "on": true, "paused": false},
		{"agent": "claude", "user": "bob@example.com", "plan": "Pro", "active": false, "on": false, "paused": false},
		{"agent": "claude", "user": "carol@example.com", "plan": "Pro", "active": false, "on": true, "paused": false},
	}
	f.magpie.mu.Unlock()
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	if !strings.Contains(body, "1 of 2 accounts in use") {
		t.Errorf("the page doesn't count the disabled account out:\n%s", body)
	}
	if !strings.Contains(body, "disabled") {
		t.Errorf("the page doesn't say an account is disabled:\n%s", body)
	}
	// alice, in use, before bob, disabled
	if i, j := strings.Index(body, "alice@example.com"), strings.Index(body, "bob@example.com"); i < 0 || j < 0 || i > j {
		t.Errorf("the accounts aren't in use first:\n%s", body)
	}
}

// TestAccountsPageKeepsAKeyApart: a group holding an API key draws it as a
// key, without an allowance that isn't there.
func TestAccountsPageKeepsAKeyApart(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, "claude/9999999999999999"}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	if !strings.Contains(body, "relay-key") {
		t.Errorf("the page doesn't show the group's key:\n%s", body)
	}
	if !strings.Contains(body, "API key, not a signed-in account") {
		t.Errorf("the page doesn't say it is a key:\n%s", body)
	}
}

// TestPageKeepsMagpiesLook is not a style test: it says the page still
// carries magpie's own palette, so a change that reinvents the colours is
// caught here rather than by an eye. The tokens are app.css's; a page that
// drifts from them reads as a different product beside the app.
func TestPageKeepsMagpiesLook(t *testing.T) {
	f := newFixture(t)
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	for _, want := range []string{
		"--accent:#4f46e5", // app.css's accent, light
		"--accent:#a5b4fc", // and dark
		"--bg:#f4f4f6",     //
		"--card:#232327",   // the dark card
		"--amber:#b45309",  // a window at 100%
		"--green:#189a58",  // in use
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page no longer carries magpie's %q", want)
		}
	}
	// and it is drawn as magpie draws a subscription: a card, the windows
	// in a two-column grid, a 4px bar under each
	for _, want := range []string{"class=\"cards\"", "class=\"card", "class=\"wins", "class=\"track", "class=\"labels\""} {
		if !strings.Contains(body, want) {
			t.Errorf("the page no longer uses magpie's %s", want)
		}
	}
	if !strings.Contains(body, "height:4px; border-radius:2px; background:var(--pill)") {
		t.Error("the window's bar is no longer magpie's 4px track")
	}
}
