package teamd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// issueMember posts the members form, as the page does.
func issueMember(t *testing.T, f *fixture, token, name, ttl string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, f.srv, "/team/"+token+"/members", url.Values{"name": {name}, "ttl": {ttl}})
}

// membersText is the members page as a reader gets it.
func membersText(t *testing.T, f *fixture, token string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/team/"+token+"/members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("members page answered %d: %s", rec.Code, rec.Body.String())
	}
	return pageText(rec.Body.String())
}

// TestIssuingAMemberKeyShowsItOnce: the form makes a key under the team's
// own key, the page shows it the once, and a later draw never does.
func TestIssuingAMemberKeyShowsItOnce(t *testing.T) {
	f := newFixture(t)
	rec := issueMember(t, f, f.tokenA, "Alice", "7d")
	if rec.Code != http.StatusOK {
		t.Fatalf("issuing answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := f.magpie.issuedUnder(); len(got) != 1 || got[0] != keyA {
		t.Fatalf("magpie was asked to issue under %q, want the team's own key %s", got, keyA)
	}
	secret := f.magpie.lastIssuedKey()
	if secret == "" || !strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("the page doesn't show the key it just made:\n%s", rec.Body.String())
	}
	if ttl := f.magpie.lastIssuedTTL(); ttl != 7*24*time.Hour {
		t.Errorf("issued for %s, want 7 days", ttl)
	}
	// a second draw of the page has no key in it
	text := membersText(t, f, f.tokenA)
	if strings.Contains(text, secret) {
		t.Fatal("the list shows the secret again")
	}
	if !strings.Contains(text, "Alice") {
		t.Fatalf("the list doesn't name the key:\n%s", text)
	}
}

// TestMemberKeyLifetimes: the four choices the page offers, and nothing
// else.
func TestMemberKeyLifetimes(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct {
		ttl  string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"90d", 90 * 24 * time.Hour},
		{"forever", 100 * 365 * 24 * time.Hour},
	} {
		if rec := issueMember(t, f, f.tokenA, "member "+c.ttl, c.ttl); rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d: %s", c.ttl, rec.Code, rec.Body.String())
		}
		if got := f.magpie.lastIssuedTTL(); got != c.want {
			t.Errorf("%s issued for %s, want %s", c.ttl, got, c.want)
		}
	}
	for _, bad := range []string{"", "1h", "1y", "-7d", "999999d"} {
		if rec := issueMember(t, f, f.tokenA, "bad "+bad, bad); rec.Code == http.StatusOK {
			t.Errorf("ttl %q was taken", bad)
		}
	}
}

// TestPermanentReadsAsPermanent: the hundred-year key says 永久 on the
// page, never a date in 2126.
func TestPermanentReadsAsPermanent(t *testing.T) {
	f := newFixture(t)
	f.magpie.expiresForNextIssue = time.Now().Add(100 * 365 * 24 * time.Hour)
	issueMember(t, f, f.tokenA, "Alice", "forever")
	text := membersText(t, f, f.tokenA)
	if !strings.Contains(text, "永久") && !strings.Contains(text, "permanent") {
		t.Fatalf("a hundred-year key isn't shown as permanent:\n%s", text)
	}
}

// TestRevokingAMemberKey: a team's own key goes, another team's does not.
func TestRevokingAMemberKey(t *testing.T) {
	f := newFixture(t)
	f.magpie.issued = []MemberKey{{ID: "m1", Name: "Alice", Issuer: keyA, Expires: time.Now().Add(24 * time.Hour)}}
	rec := postForm(t, f.srv, "/team/"+f.tokenA+"/members/m1/revoke", url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoking answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := f.magpie.revokedUnder(); len(got) != 1 || got[0] != keyA+"/m1" {
		t.Fatalf("magpie was asked to revoke %q", got)
	}
	// another team's link can't revoke it
	f.magpie.issued = []MemberKey{{ID: "m1", Name: "Alice", Issuer: keyA, Expires: time.Now().Add(24 * time.Hour)}}
	f.magpie.refuseRevoke = true
	if rec := postForm(t, f.srv, "/team/"+f.tokenB+"/members/m1/revoke", url.Values{}); rec.Code == http.StatusOK {
		t.Fatal("another team revoked a key through its own link")
	}
}

// TestMembersPageNeverReachesAnotherTeam: the issuer is the team's own,
// whatever a request says.
func TestMembersPageNeverReachesAnotherTeam(t *testing.T) {
	f := newFixture(t)
	postForm(t, f.srv, "/team/"+f.tokenB+"/members",
		url.Values{"name": {"Alice"}, "ttl": {"7d"}, "issuer": {keyA}})
	for _, got := range f.magpie.issuedUnder() {
		if got != keyB {
			t.Fatalf("a request naming an issuer had it used: %q", got)
		}
	}
}

// TestMembersPageIsAudited: one line for the issue and one for the revoke.
func TestMembersPageIsAudited(t *testing.T) {
	f := newFixture(t)
	issueMember(t, f, f.tokenA, "Alice", "7d")
	f.magpie.issued = []MemberKey{{ID: "m1", Name: "Alice", Issuer: keyA, Expires: time.Now().Add(24 * time.Hour)}}
	postForm(t, f.srv, "/team/"+f.tokenA+"/members/m1/revoke", url.Values{})
	file := f.auditOnDisk(t)
	if !strings.Contains(file, actKeyIssue) || !strings.Contains(file, actKeyRevoke) {
		t.Fatalf("the audit doesn't carry both actions:\n%s", file)
	}
	if !strings.Contains(file, "Alice") {
		t.Fatalf("the audit doesn't name the member:\n%s", file)
	}
}

// TestMembersPageRefusesATeamWhoseKeyCantIssue: the form says so, and the
// post changes nothing.
func TestMembersPageRefusesATeamWhoseKeyCantIssue(t *testing.T) {
	f := newFixture(t)
	f.magpie.keys[0].Issues = false
	text := membersText(t, f, f.tokenA)
	if !strings.Contains(text, "gateway-key issuer") {
		t.Fatalf("the page doesn't say how to let the key issue:\n%s", text)
	}
	if rec := issueMember(t, f, f.tokenA, "Alice", "7d"); rec.Code == http.StatusOK {
		t.Fatal("issued under a key that may not")
	}
	if len(f.magpie.issuedUnder()) != 0 {
		t.Fatal("magpie was asked to issue anyway")
	}
}
