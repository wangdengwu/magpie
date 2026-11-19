package teamd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// postForm posts a form to the portal the way a browser does, with the
// Origin a browser sends.
func postForm(t *testing.T, s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+r.Host)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

// startSignIn takes a team's add page to a sign-in, as the form does.
func startSignIn(t *testing.T, f *fixture) SignIn {
	t.Helper()
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/add", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("starting a sign-in answered %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	id := loc[strings.LastIndex(loc, "/")+1:]
	st, err := f.srv.SignInOf(context.Background(), mustGrant(t, f, f.tokenA), id)
	if err != nil {
		t.Fatalf("the sign-in %q the portal started can't be read: %v", id, err)
	}
	return st
}

// mustGrant is the boundary's answer for a token, for the tests that call
// the server's methods directly.
func mustGrant(t *testing.T, f *fixture, token string) Grant {
	t.Helper()
	g, err := f.srv.Authorize(context.Background(), token, Target{})
	if err != nil {
		t.Fatalf("the token was refused: %v", err)
	}
	return g
}

// TestAddFlowPutsTheAccountInTheTeamsGroup is the whole slice: start a
// sign-in, paste what the browser ended on, and the account is in this
// team's group — and in no other's.
func TestAddFlowPutsTheAccountInTheTeamsGroup(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	if !strings.HasPrefix(st.URL, "https://claude.com/") {
		t.Fatalf("the sign-in's link is %q, want the vendor's page", st.URL)
	}
	if !st.Waiting() {
		t.Fatalf("a new sign-in is %q, want waiting", st.State)
	}

	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("abc123")}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the paste answered %d: %s", rec.Code, rec.Body.String())
	}
	if to := rec.Header().Get("Location"); !strings.Contains(to, "?added=") {
		t.Errorf("the paste sent the browser to %q, want the pool with the account named", to)
	}

	groups := f.groups(t)
	if !slicesContainsFold(groups[groupA], "claude/"+userID("new@example.com")) {
		t.Errorf("team A's group is %q, which doesn't hold the new account", groups[groupA])
	}
	if len(groups[groupB]) != 1 || !slicesContainsFold(groups[groupB], acctB1) {
		t.Errorf("team B's group changed: %q", groups[groupB])
	}
	// and the page then shows it
	body := get(t, f.srv, "/team/"+f.tokenA).Body.String()
	if !strings.Contains(body, "new@example.com") {
		t.Errorf("the pool's page doesn't show the new account:\n%s", body)
	}
}

// TestSignInIsTheTeamsOwn is the boundary this slice adds: magpie's
// sign-ins are global, so the portal must not let one team read, finish or
// cancel another's.
func TestSignInIsTheTeamsOwn(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)

	// team B's token can't see it, paste into it, or cancel it
	if rec := get(t, f.srv, "/team/"+f.tokenB+"/signin/"+st.ID); rec.Code != http.StatusNotFound {
		t.Errorf("team B reading team A's sign-in answered %d, want 404", rec.Code)
	}
	rec := postForm(t, f.srv, "/team/"+f.tokenB+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("stolen")}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("team B pasting into team A's sign-in answered %d, want 404", rec.Code)
	}
	if rec := postForm(t, f.srv, "/team/"+f.tokenB+"/signin/"+st.ID+"/cancel", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("team B cancelling team A's sign-in answered %d, want 404", rec.Code)
	}
	// and nothing of team A's was touched
	if pastes := f.magpie.pastesTaken(); len(pastes) != 0 {
		t.Errorf("another team's paste reached magpie: %q", pastes)
	}
	if canceled := f.magpie.canceledIDs(); len(canceled) != 0 {
		t.Errorf("another team cancelled the sign-in: %q", canceled)
	}
	// it still finishes for the team that started it
	rec = postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("mine")}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the team's own paste answered %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSignInOfAnotherPortalIsRefused: a sign-in magpie has but this portal
// didn't start is nobody's here — the id alone is not a way in.
func TestSignInOfAnotherPortalIsRefused(t *testing.T) {
	f := newFixture(t)
	// magpie has a sign-in this portal never started, as another magpie's
	// admin key could have made
	f.magpie.mu.Lock()
	f.magpie.signIns["someone-elses"] = map[string]any{
		"id": "someone-elses", "agent": "claude", "state": "waiting",
		"url": "https://claude.com/cai/oauth/authorize?code=true&client_id=x&state=z",
	}
	f.magpie.mu.Unlock()
	if rec := get(t, f.srv, "/team/"+f.tokenA+"/signin/someone-elses"); rec.Code != http.StatusNotFound {
		t.Errorf("a sign-in this portal didn't start answered %d, want 404", rec.Code)
	}
}

// TestBadPasteIsRefusedAndSaysWhy: an address from another sign-in is
// refused with magpie's words, and the sign-in goes on.
func TestBadPasteIsRefusedAndSaysWhy(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {"https://example.com/?code=x"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a foreign address answered %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "isn&#39;t from this sign-in") && !strings.Contains(rec.Body.String(), "isn't from this sign-in") {
		t.Errorf("the page doesn't say why it was refused:\n%s", rec.Body.String())
	}
	// the sign-in still waits, and the team's group took nothing
	if groups := f.groups(t); len(groups[groupA]) != 2 {
		t.Errorf("a refused paste changed the group: %q", groups[groupA])
	}
	if _, err := f.srv.SignInOf(context.Background(), mustGrant(t, f, f.tokenA), st.ID); err != nil {
		t.Errorf("the sign-in was dropped by a refused paste: %v", err)
	}
}

// TestCancelStopsTheSignIn: cancelling reaches magpie and forgets it here.
func TestCancelStopsTheSignIn(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/cancel", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("cancelling answered %d: %s", rec.Code, rec.Body.String())
	}
	if canceled := f.magpie.canceledIDs(); len(canceled) != 1 || canceled[0] != st.ID {
		t.Errorf("magpie was told to cancel %q, want %q", canceled, st.ID)
	}
	// and the portal forgot it, so its page is gone
	if rec := get(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID); rec.Code != http.StatusNotFound {
		t.Errorf("a cancelled sign-in's page answered %d, want 404", rec.Code)
	}
}

// TestSignInExpires: a link nobody came back with is cancelled at magpie
// and forgotten, so magpie isn't left holding a login run.
func TestSignInExpires(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	// the janitor's clock, moved past the sign-in's life
	f.srv.now = func() time.Time { return time.Now().Add(signInTTL + time.Minute) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.srv.sweepSignIns(ctx, 10*time.Millisecond) }()
	// wait for the janitor to have swept, not for a fixed time
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(f.magpie.canceledIDs()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if canceled := f.magpie.canceledIDs(); len(canceled) != 1 || canceled[0] != st.ID {
		t.Fatalf("magpie was told to cancel %q, want the stale sign-in %q", canceled, st.ID)
	}
	if f.srv.signIns.ownedBy(st.ID, "team-a") {
		t.Error("the stale sign-in is still this portal's")
	}
}

// TestAddingTheSameAccountTwiceLeavesOneEntry: signing the same account in
// again renews it and leaves the group as it was.
func TestAddingTheSameAccountTwiceLeavesOneEntry(t *testing.T) {
	f := newFixture(t)
	// magpie signs in an account team A's group already holds
	f.magpie.mu.Lock()
	f.magpie.signed["si-1"] = "alice@example.com"
	f.magpie.mu.Unlock()

	st := startSignIn(t, f)
	if st.ID != "si-1" {
		t.Fatalf("the fake made sign-in %q, want si-1", st.ID)
	}
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("again")}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the paste answered %d: %s", rec.Code, rec.Body.String())
	}
	accounts := f.groups(t)[groupA]
	n := 0
	for _, a := range accounts {
		if strings.EqualFold(a, acctA1) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the group holds %q: the account is in it %d times, want once", accounts, n)
	}
}

// TestTheFormMustComeFromThePortalsOwnPage: a page on another site can't
// post the add form with a token it happens to know.
func TestTheFormMustComeFromThePortalsOwnPage(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/team/" + f.tokenA + "/add", "/team/" + f.tokenA + "/signin/x/callback", "/team/" + f.tokenA + "/signin/x/cancel"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("url=x"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a post from another origin to %s answered %d, want 403", path, rec.Code)
		}
	}
}

// TestTheAddPagesShowWhatTheSignInNeeds: the page a lead works from has the
// link, the paste field, and the way out.
func TestTheAddPagesShowWhatTheSignInNeeds(t *testing.T) {
	f := newFixture(t)
	body := get(t, f.srv, "/team/"+f.tokenA+"/add").Body.String()
	for _, want := range []string{"Add an account", "Start", `/team/` + f.tokenA + `/add`, "localhost:"} {
		if !strings.Contains(body, want) {
			t.Errorf("the add page doesn't say %q:\n%s", want, body)
		}
	}
	st := startSignIn(t, f)
	body = get(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID).Body.String()
	// the link is escaped in the markup, so it is checked in parts: what a
	// browser shows and a person copies is the whole address
	for _, want := range []string{"claude.com/cai/oauth/authorize", "code=true", `name="url"`, "/callback", "/cancel", "Finish"} {
		if !strings.Contains(body, want) {
			t.Errorf("the sign-in page doesn't say %q:\n%s", want, body)
		}
	}
	// another team's sign-in isn't a page at all
	if rec := get(t, f.srv, "/team/"+f.tokenB+"/signin/"+st.ID); rec.Code != http.StatusNotFound {
		t.Errorf("another team's sign-in answered %d, want 404", rec.Code)
	}
}

// TestNoPasteIsEchoedBack: the address a browser ended on carries a code,
// and it is never written into the page a failed paste is answered with.
func TestNoPasteIsEchoedBack(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	const code = "SECRET-CODE-0123456789"
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback",
		url.Values{"url": {"https://wrong.example/?code=" + code}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a foreign address answered %d", rec.Code)
	}
	for _, body := range []string{rec.Body.String(), get(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID).Body.String()} {
		if strings.Contains(body, code) {
			t.Errorf("the pasted code reached a page:\n%s", body)
		}
	}
}

// TestAddKeepsWhatOpsChangedDuringTheSignIn: a sign-in takes minutes, and
// the group is read again when the account is written — so an account ops
// added meanwhile stays, and the save is a read-modify-write of the group as
// it is now, not of the reading the page was drawn from.
func TestAddKeepsWhatOpsChangedDuringTheSignIn(t *testing.T) {
	f := newFixture(t)
	st := startSignIn(t, f)
	// ops adds an account to team A's group while this request is waiting on
	// the sign-in: the paste's call to magpie blocks until the browser's
	// sign-in is over, which is minutes
	f.magpie.mu.Lock()
	f.magpie.onCallback = func(f *fakeMagpie) {
		for i := range f.groups {
			if f.groups[i].ID == groupA {
				f.groups[i].Accounts = append(f.groups[i].Accounts, acctOps)
			}
		}
	}
	f.magpie.mu.Unlock()

	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("late")}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the paste answered %d: %s", rec.Code, rec.Body.String())
	}
	accounts := f.groups(t)[groupA]
	if !slicesContainsFold(accounts, acctOps) {
		t.Errorf("the account ops added during the sign-in was lost: %q", accounts)
	}
	if !slicesContainsFold(accounts, "claude/"+userID("new@example.com")) {
		t.Errorf("the signed-in account isn't in the group: %q", accounts)
	}
	for _, kept := range []string{acctA1, acctA2} {
		if !slicesContainsFold(accounts, kept) {
			t.Errorf("the group lost %q: %q", kept, accounts)
		}
	}
}

// TestAddedBannerOnlyNamesTheTeamsOwnAccount: ?added is in a URL, which
// anyone can type, so it puts nothing on the page its own group doesn't hold.
func TestAddedBannerOnlyNamesTheTeamsOwnAccount(t *testing.T) {
	f := newFixture(t)
	body := get(t, f.srv, "/team/"+f.tokenA+"?added="+acctA1).Body.String()
	if !strings.Contains(body, "Added ") {
		t.Errorf("the page doesn't say the team's own account was added:\n%s", body)
	}
	for _, foreign := range []string{acctB1, acctA1 + "x", "claude/ffffffffffffffff"} {
		body := get(t, f.srv, "/team/"+f.tokenA+"?added="+url.QueryEscape(foreign)).Body.String()
		if strings.Contains(body, "Added ") {
			t.Errorf("a URL naming %q put an \"Added\" line on the page:\n%s", foreign, body)
		}
	}
	// and another team's account is never named, in the banner or anywhere
	body = get(t, f.srv, "/team/"+f.tokenA+"?added="+acctB1).Body.String()
	if strings.Contains(body, acctB1) || strings.Contains(body, "carol@example.com") {
		t.Errorf("a URL naming another team's account put it on the page:\n%s", body)
	}
}
