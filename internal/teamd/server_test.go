package teamd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// healthOf asks the server's health endpoint and gives the answer.
func healthOf(t *testing.T, s *Server) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("/healthz answered %d with %q, which doesn't read: %v", rec.Code, rec.Body.String(), err)
	}
	return rec.Code, body
}

// TestHealthSaysTeamdCanWork: with its teams readable and magpie answering,
// the sidecar is up.
func TestHealthSaysTeamdCanWork(t *testing.T) {
	f := newFixture(t)
	code, body := healthOf(t, f.srv)
	if code != http.StatusOK {
		t.Fatalf("/healthz answered %d: %v", code, body)
	}
	if body["ok"] != true {
		t.Errorf("/healthz says %v, want ok", body)
	}
	if n, _ := body["teams"].(float64); n != 2 {
		t.Errorf("/healthz counts %v teams, want 2", body["teams"])
	}
	if body["magpie"] != "ok" {
		t.Errorf("/healthz says magpie is %v, want ok", body["magpie"])
	}
}

// TestHealthSaysWhenMagpieIsDown: a magpie that doesn't answer is said, but
// it does not fail the check — restarting this container would not help it.
func TestHealthSaysWhenMagpieIsDown(t *testing.T) {
	f := newFixture(t)
	f.magpie.fail(500, `{"error":"no"}`)
	code, body := healthOf(t, f.srv)
	if code != http.StatusOK {
		t.Fatalf("/healthz answered %d while magpie was down: %v", code, body)
	}
	if body["magpie"] != "unreachable" {
		t.Errorf("/healthz says magpie is %v, want unreachable", body["magpie"])
	}
}

// TestHealthFailsWhenTheTeamsCantBeRead: the one state in which teamd
// cannot serve at all, since every page starts at the teams.
func TestHealthFailsWhenTheTeamsCantBeRead(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.srv.Store().Dir(), teamsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := healthOf(t, f.srv)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("/healthz answered %d, want 503: %v", code, body)
	}
	if body["ok"] != false {
		t.Errorf("/healthz says %v, want not ok", body)
	}
}

// TestEveryAnswerIsUncacheable: a token is in the path of every page, so
// nothing the portal says may be kept by a browser or a proxy, and no
// referrer may carry the path on.
func TestEveryAnswerIsUncacheable(t *testing.T) {
	f := newFixture(t)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy is %q, want no-referrer", got)
	}
	// and a path nobody serves is answered too, with the same headers
	rec = httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/some-token", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/team/some-token answered %d, want 404 in this slice", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a 404's Cache-Control is %q, want no-store", got)
	}
}

// TestAnOversizedFormIsRefusedUnread: a post bigger than any form the
// portal draws is refused before it is read whole. Go's own form cap is
// 10 MiB a request, so a few dozen at once would take a sidecar held to a
// few hundred MiB out of memory, and with it the Pod's gateway.
func TestAnOversizedFormIsRefusedUnread(t *testing.T) {
	f := newFixture(t)
	form := url.Values{"account": {acctA2}, "pad": {strings.Repeat("x", 1<<20)}}
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/accounts/off", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a 1 MiB form answered %d, want 400: %.200s", rec.Code, rec.Body.String())
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Fatalf("magpie was told %q for a form that was refused", logins)
	}
	// and a form the page really sends still goes through
	if rec := switchPosts(t, f, f.tokenA, "off", acctA2); rec.Code != http.StatusOK {
		t.Fatalf("a normal form answered %d: %s", rec.Code, rec.Body.String())
	}
}
