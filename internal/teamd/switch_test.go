package teamd

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// switchPosts is what the pool's page sends when a lead presses a control,
// as a browser sends it: a form with the account in it, from the portal's
// own origin.
func switchPosts(t *testing.T, f *fixture, token, action, account string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, f.srv, "/team/"+token+"/accounts/"+action, url.Values{"account": {account}})
}

// pageText is a page as a reader gets it: the markup's escapes undone, so a
// test can look for the words rather than for how they were escaped. The
// escaping itself is html/template's, and page_test.go checks it.
func pageText(s string) string { return html.UnescapeString(s) }

// TestDisablingAnAccountStopsItServing: the control does what it says. A
// disabled account is off in magpie, the page says so, and enabling it puts
// it back.
func TestDisablingAnAccountStopsItServing(t *testing.T) {
	f := newFixture(t)
	rec := switchPosts(t, f, f.tokenA, "off", acctA2)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabling answered %d: %s", rec.Code, rec.Body.String())
	}
	// magpie was told, on the agent and the sign-in — never on the group's id
	if logins := f.magpie.loginsTaken(); len(logins) != 1 || logins[0] != "off claude bob@example.com" {
		t.Fatalf("magpie was told %q, want one \"off claude bob@example.com\"", logins)
	}
	if !strings.Contains(pageText(rec.Body.String()), "is disabled: the team's requests won't use it") {
		t.Errorf("the page doesn't say the account is disabled:\n%s", rec.Body.String())
	}
	// and the page drawn from magpie says it, not the request
	if !strings.Contains(pageText(rec.Body.String()), "1 of 2 accounts in use") {
		t.Errorf("the page still counts the disabled account in use:\n%s", rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "disabled</span>") {
		t.Errorf("the disabled account's chip isn't drawn as disabled:\n%s", rec.Body.String())
	}

	// back on
	rec = switchPosts(t, f, f.tokenA, "on", acctA2)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabling answered %d: %s", rec.Code, rec.Body.String())
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 2 || logins[1] != "on claude bob@example.com" {
		t.Fatalf("magpie was told %q, want \"on claude bob@example.com\" second", logins)
	}
	if !strings.Contains(pageText(rec.Body.String()), "2 of 2 accounts in use") {
		t.Errorf("the page doesn't count the account in use again:\n%s", rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "is in the team's pool again") {
		t.Errorf("the page doesn't say it is back:\n%s", rec.Body.String())
	}
}

// TestTheSwitchStateIsMagpiesAfterAReload: what the control says is read
// from magpie, so a reload after ops changed an account by hand shows what
// magpie has, not what the portal last did.
func TestTheSwitchStateIsMagpiesAfterAReload(t *testing.T) {
	f := newFixture(t)
	// ops turns bob off in magpie's own app
	f.magpie.setLogins(t, "alice@example.com:on::active", "bob@example.com:::", "carol@example.com:on::")
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	if !strings.Contains(pageText(body), "1 of 2 accounts in use") {
		t.Errorf("the page doesn't read the state from magpie:\n%s", body)
	}
	// bob's row is the disabled one and carries Enable; alice's carries Disable
	if !strings.Contains(body, `name="account" value="`+acctA2+`">`) {
		t.Errorf("bob's row has no control:\n%s", body)
	}
	alice := body[strings.Index(body, "alice@example.com"):]
	if i := strings.Index(alice, "</section>"); i > 0 {
		alice = alice[:i]
	}
	if !strings.Contains(alice, "Disable") {
		t.Errorf("the account in use has no Disable control:\n%s", alice)
	}
	if strings.Contains(alice, ">Enable<") {
		t.Errorf("an account in use is offered Enable:\n%s", alice)
	}
}

// TestSwitchItselfRefusesAGrantOfAnotherGroup: the write's own promise, not
// only the route's. A caller that made a grant for team A and named team B's
// account is refused, and magpie is never told — the route already checks
// this, so a route test can't show that Switch does too.
func TestSwitchItselfRefusesAGrantOfAnotherGroup(t *testing.T) {
	f := newFixture(t)
	g := mustGrant(t, f, f.tokenA)
	for _, entry := range []string{acctB1, acctB1 + "x", "claude/ffffffffffffffff", "", "carol@example.com"} {
		if err := f.srv.Switch(t.Context(), g, entry, false); !errors.Is(err, ErrNoAccount) {
			t.Errorf("Switch(%q) answered %v, want ErrNoAccount", entry, err)
		}
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("Switch told magpie to change an account outside the grant: %q", logins)
	}
}

// TestAnotherTeamsAccountIsRefused is the boundary: a lead's link can't
// switch an account of another team's group, and magpie is never told.
func TestAnotherTeamsAccountIsRefused(t *testing.T) {
	f := newFixture(t)
	for _, action := range []string{"off", "on"} {
		for _, account := range []string{acctB1, acctB1 + "x", "claude/ffffffffffffffff", "carol@example.com", "", "@" + groupB, "claude/1a2b/extra"} {
			rec := switchPosts(t, f, f.tokenA, action, account)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s on %q answered %d, want 403", action, account, rec.Code)
			}
		}
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("a foreign account reached magpie: %q", logins)
	}
	// and team B's group and account are as they were
	if got := f.groups(t)[groupB]; len(got) != 1 || !slicesContainsFold(got, acctB1) {
		t.Errorf("team B's group changed: %q", got)
	}
}

// TestAnAccountOpsTookOutIsRefused: the group is read for the request that
// acts, so an account ops removed from it since the page was drawn is
// refused rather than switched.
func TestAnAccountOpsTookOutIsRefused(t *testing.T) {
	f := newFixture(t)
	// the page the lead is looking at has bob on it
	if body := get(t, f.srv, "/team/"+f.tokenA).Body.String(); !strings.Contains(pageText(body), "bob@example.com") {
		t.Fatalf("the page doesn't show bob to begin with:\n%s", body)
	}
	// ops takes bob out of the group, and the lead presses the button the
	// stale page still carries
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	rec := switchPosts(t, f, f.tokenA, "off", acctA2)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("an account the group no longer holds answered %d, want 403", rec.Code)
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("magpie was told to switch an account out of the group: %q", logins)
	}
}

// TestAnAccountSignedOutSinceIsRefused: the group still lists it, magpie has
// no account by that id, and switching it is not a change anyone can make.
// A lead's own page says so; magpie is never told.
func TestAnAccountSignedOutSinceIsRefused(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, "claude/ffffffffffffffff"}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	// the stale page carries no control for it, and its note says magpie has
	// no account by the id
	body := pageText(get(t, f.srv, "/team/"+f.tokenA).Body.String())
	if !strings.Contains(body, "no account magpie has now") {
		t.Errorf("the page doesn't say the entry matches nothing:\n%s", body)
	}
	if strings.Contains(sectionOf(body, "claude/ffffffffffffffff"), "<form") {
		t.Errorf("the lost entry's row carries a control:\n%s", sectionOf(body, "claude/ffffffffffffffff"))
	}
	// and a post that asks anyway says why, in the portal's own words
	rec := switchPosts(t, f, f.tokenA, "off", "claude/ffffffffffffffff")
	if rec.Code != http.StatusOK {
		t.Fatalf("an entry magpie lost answered %d, want 200 with a notice: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "Magpie has no account by that id any more") {
		t.Errorf("the page doesn't say the entry has nothing to switch:\n%s", rec.Body.String())
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("an account magpie doesn't have reached magpie: %q", logins)
	}
}

// TestTheLastAccountInUseSaysWhatDisablingItDoes: a pool of one warns
// before the click, and magpie's refusal to pause its only account is said
// in magpie's own words.
func TestTheLastAccountInUseSaysWhatDisablingItDoes(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	if !strings.Contains(pageText(body), "The only account the team has in use") {
		t.Errorf("the page doesn't warn before the click:\n%s", body)
	}

	// the account its agent is signed in to can't be paused while it is the
	// only one on anywhere — magpie's rule counts every account of the
	// agent, not only this team's — and magpie says so in its own words
	f.magpie.setLogins(t, "alice@example.com:on::active", "bob@example.com:::", "carol@example.com:::")
	rec := switchPosts(t, f, f.tokenA, "off", acctA1)
	if rec.Code != http.StatusOK {
		t.Fatalf("the refusal answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "turn another on first to pause it") {
		t.Errorf("the page doesn't carry magpie's own words for the refusal:\n%s", rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "1 of 1 account in use") {
		t.Errorf("the page doesn't say the account is still in use:\n%s", rec.Body.String())
	}
}

// TestDisablingTheOnlySavedAccountEmptiesThePool is the other half: the
// account magpie can pause is not the one its agent is signed in to, so it
// goes off and the page says the team has nothing left.
func TestDisablingTheOnlySavedAccountEmptiesThePool(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA2}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	f.magpie.setLogins(t, "alice@example.com:on::active", "bob@example.com:on::")
	rec := switchPosts(t, f, f.tokenA, "off", acctA2)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabling answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "is disabled: the team's requests won't use it") {
		t.Errorf("the page doesn't say the account is disabled:\n%s", rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "0 of 1 account in use") {
		t.Errorf("the page doesn't say the team has nothing left:\n%s", rec.Body.String())
	}
}

// TestASwitchFromAnotherOriginIsRefused: a page on another site can't press
// a control with a token it happens to know.
func TestASwitchFromAnotherOriginIsRefused(t *testing.T) {
	f := newFixture(t)
	for _, action := range []string{"off", "on"} {
		r := httptest.NewRequest(http.MethodPost, "/team/"+f.tokenA+"/accounts/"+action,
			strings.NewReader(url.Values{"account": {acctA1}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from another origin answered %d, want 403", action, rec.Code)
		}
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("a page on another site switched an account: %q", logins)
	}
}

// TestThereIsNoWayToRemoveAnAccount: the portal's own routes are the whole
// surface, and none of them takes an account out of a group or forgets a
// sign-in. This is the promise stated as a test, since the route that would
// break it is one line to add.
func TestThereIsNoWayToRemoveAnAccount(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{
		"/team/" + f.tokenA + "/accounts/forget",
		"/team/" + f.tokenA + "/accounts/remove",
		"/team/" + f.tokenA + "/accounts/delete",
		"/team/" + f.tokenA + "/accounts/" + acctA1 + "/forget",
		"/team/" + f.tokenA + "/login/forget",
		"/team/" + f.tokenA + "/group/remove",
	} {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			r := httptest.NewRequest(method, path, strings.NewReader(url.Values{"account": {acctA1}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			f.srv.Handler().ServeHTTP(rec, r)
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s answered %d: a route that removes an account exists", method, path, rec.Code)
			}
		}
	}
	// and the pool's page offers no such control: every form it carries is
	// one of the two switches
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	for _, form := range strings.Split(body, "<form")[1:] {
		if i := strings.Index(form, ">"); i > 0 {
			form = form[:i]
		}
		if !strings.Contains(form, "/accounts/off") && !strings.Contains(form, "/accounts/on") {
			t.Errorf("the page carries a form that isn't a switch: <form%s", form)
		}
	}
	// nothing was removed: the group is as it was
	if got := f.groups(t)[groupA]; len(got) != 2 {
		t.Errorf("the group changed: %q", got)
	}
}

// TestThePageSaysWhichRowsCantBeSwitched: a key and an entry magpie lost
// carry no control. The key's row says why; the lost entry's note already
// says magpie has no account by that id, and its row doesn't say it twice.
func TestThePageSaysWhichRowsCantBeSwitched(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, "claude/9999999999999999", "claude/ffffffffffffffff"}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	body := pageText(get(t, f.srv, "/team/"+f.tokenA).Body.String())
	if !strings.Contains(body, "An API key has no sign-in to switch") {
		t.Errorf("the page doesn't say a key can't be switched:\n%s", body)
	}
	// the key's entry has no form of its own, and neither has the lost one
	for _, entry := range []string{"claude/9999999999999999", "claude/ffffffffffffffff"} {
		if strings.Contains(sectionOf(body, entry), "<form") {
			t.Errorf("the row for %s carries a control:\n%s", entry, sectionOf(body, entry))
		}
	}
	// and the lost entry is not told its fault twice
	if n := strings.Count(sectionOf(body, "claude/ffffffffffffffff"), "magpie has no account"); n > 1 {
		t.Errorf("the lost entry's row says magpie has no account %d times:\n%s", n, sectionOf(body, "claude/ffffffffffffffff"))
	}
}

// sectionOf is the card one entry is drawn in, for a test that counts what
// a row says. The cards are drawn in one grid, so a card's own bytes run
// from its <section> to the next one's.
func sectionOf(body, entry string) string {
	i := strings.Index(body, ">"+entry+"<")
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(body[:i], "<section")
	end := strings.Index(body[i:], "<section")
	if start < 0 {
		return body[i:]
	}
	if end < 0 {
		return body[start:]
	}
	return body[start : i+end]
}

// TestThePageSaysWhatMagpieDidNotWhatWasAsked: magpie can take a change and
// still serve nothing from the account — a vendor that rested it, an
// account paused behind another. The page reads that back from magpie, so
// it never says "disabled" over an account magpie still serves from.
func TestThePageSaysWhatMagpieDidNotWhatWasAsked(t *testing.T) {
	f := newFixture(t)
	// magpie answers 204 and changes nothing: the case a page that believed
	// its own request would get wrong
	f.magpie.mu.Lock()
	f.magpie.onLogin = func(f *fakeMagpie, on bool, user string) string {
		f.providers[0]["account"].(map[string]any)["logins"] = []map[string]any{
			{"agent": "claude", "user": "alice@example.com", "plan": "Max", "active": true, "on": true, "paused": false},
			{"agent": "claude", "user": "bob@example.com", "plan": "Pro", "active": false, "on": true, "paused": false},
			{"agent": "claude", "user": "carol@example.com", "plan": "Pro", "active": false, "on": true, "paused": false},
		}
		return ""
	}
	f.magpie.mu.Unlock()

	rec := switchPosts(t, f, f.tokenA, "off", acctA2)
	if rec.Code != http.StatusOK {
		t.Fatalf("the switch answered %d: %s", rec.Code, rec.Body.String())
	}
	body := pageText(rec.Body.String())
	if strings.Contains(body, "is disabled: the team's requests") {
		t.Errorf("the page says the account is disabled, and magpie still serves from it:\n%s", body)
	}
	if !strings.Contains(body, "is still in use") {
		t.Errorf("the page doesn't say the account is still in use:\n%s", body)
	}
	if !strings.Contains(body, "2 of 2 accounts in use") {
		t.Errorf("the page's count doesn't come from magpie:\n%s", body)
	}
}

// TestASwitchThatMagpieRefusesSaysSoInItsWords: the page carries magpie's
// reason, since it is the only thing a lead can act on.
func TestASwitchThatMagpieRefusesSaysSoInItsWords(t *testing.T) {
	f := newFixture(t)
	f.magpie.mu.Lock()
	f.magpie.onLogin = func(f *fakeMagpie, on bool, user string) string {
		return "the vendor has this account rested until 12:00"
	}
	f.magpie.mu.Unlock()
	rec := switchPosts(t, f, f.tokenA, "on", acctA2)
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused switch answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(pageText(rec.Body.String()), "the vendor has this account rested until 12:00") {
		t.Errorf("the page doesn't say why magpie refused:\n%s", rec.Body.String())
	}
}

// TestMagpiesErrorDoesNotGoIntoTheWrongPage: a failure that isn't magpie
// refusing the change says only that, never what magpie sent — which can
// name another team's accounts.
func TestMagpiesErrorDoesNotGoIntoTheWrongPage(t *testing.T) {
	f := newFixture(t)
	f.magpie.mu.Lock()
	f.magpie.status, f.magpie.body = 500, `{"error":"providers.json can't be read: claude/carol@example.com"}`
	f.magpie.mu.Unlock()
	// the boundary can't read the group, so nothing is switched
	rec := switchPosts(t, f, f.tokenA, "off", acctA2)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable magpie answered %d, want 503", rec.Code)
	}
	if strings.Contains(pageText(rec.Body.String()), "carol@example.com") {
		t.Errorf("magpie's own error reached the page: %s", rec.Body.String())
	}
}

// TestAFormFromThePortalsOwnPageIsTaken is the case a real browser sends,
// and the one the portal's own headers make: Referrer-Policy:
// no-referrer makes Chromium withhold Origin on a form post, so the post
// from the portal's page arrives with `Origin: null`. Judging Origin alone
// refused every switch and every sign-in from a browser while the tests,
// which sent an Origin no browser sends, all passed.
func TestAFormFromThePortalsOwnPageIsTaken(t *testing.T) {
	f := newFixture(t)
	// what Chromium sends from this portal's page: no Origin, and the
	// browser's own word for where the post came from
	r := httptest.NewRequest(http.MethodPost, "/team/"+f.tokenA+"/accounts/off",
		strings.NewReader(url.Values{"account": {acctA2}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "null")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("a form from the portal's own page answered %d: %s", rec.Code, rec.Body.String())
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 1 {
		t.Errorf("the switch didn't reach magpie: %q", logins)
	}
}

// TestAFormFromAnotherSiteIsRefusedEvenWithAClaimedOrigin: Sec-Fetch-Site
// is what decides, so a page that sends the portal's own Origin and the
// browser's `cross-site` is refused — and a sandboxed frame's `null` origin
// with it.
func TestAFormFromAnotherSiteIsRefusedEvenWithAClaimedOrigin(t *testing.T) {
	f := newFixture(t)
	for _, h := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Origin": "http://" + "127.0.0.1"},
		{"Origin": "null"},
		{"Origin": "null", "Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "none"},
		{"Origin": "https://evil.example"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/team/"+f.tokenA+"/accounts/off",
			strings.NewReader(url.Values{"account": {acctA2}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range h {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a post with %v answered %d, want 403", h, rec.Code)
		}
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("a foreign page switched an account: %q", logins)
	}
}
