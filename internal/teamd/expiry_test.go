package teamd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// linkEnds is when the fixture's team A link ends in these tests.
var linkEnds = time.Date(2026, 12, 18, 10, 0, 0, 0, time.UTC)

// expiring gives team A's link an end and holds the portal's clock at at.
func expiring(t *testing.T, f *fixture, at time.Time) {
	t.Helper()
	f.teams[0].ExpiresAt = linkEnds
	if err := f.srv.Store().Save(f.teams); err != nil {
		t.Fatal(err)
	}
	f.srv.now = func() time.Time { return at }
}

// TestAuthorizeRefusesAnExpiredLink: a link with an end works up to it and
// is no team from it on, as a rotated one is.
func TestAuthorizeRefusesAnExpiredLink(t *testing.T) {
	f := newFixture(t)
	expiring(t, f, linkEnds.Add(-time.Second))
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); err != nil {
		t.Fatalf("a link a second before its end was refused: %v", err)
	}
	f.srv.now = func() time.Time { return linkEnds }
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); !errors.Is(err, ErrNoTeam) {
		t.Errorf("a link at its end answered %v, want ErrNoTeam", err)
	}
	// the other team's link, which has none, is not touched by it
	if _, err := f.srv.Authorize(context.Background(), f.tokenB, Target{}); err != nil {
		t.Errorf("team B's link, with no end, was refused: %v", err)
	}
}

// TestALinkWithNoEndNeverEnds: teams.json from before links had an end
// reads as permanent, however far the clock goes.
func TestALinkWithNoEndNeverEnds(t *testing.T) {
	f := newFixture(t)
	f.srv.now = func() time.Time { return time.Date(2126, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := f.srv.Authorize(context.Background(), f.tokenA, Target{}); err != nil {
		t.Errorf("a link with no end was refused a century on: %v", err)
	}
}

// TestAnExpiredLinkOpensNoPage: every page and the switch go through the
// boundary, so an ended link opens none of them, and the page it gets is
// the one an unknown link gets.
func TestAnExpiredLinkOpensNoPage(t *testing.T) {
	pages := []string{"", "/members", "/audit", "/connect", "/usage", "/add"}
	f := newFixture(t)
	expiring(t, f, linkEnds.Add(-time.Second))
	for _, p := range pages {
		if rec := get(t, f.srv, "/team/"+f.tokenA+p); rec.Code == http.StatusNotFound {
			t.Errorf("GET %s a second before the end answered 404", p)
		}
	}
	f.srv.now = func() time.Time { return linkEnds.Add(time.Second) }
	for _, p := range pages {
		rec := get(t, f.srv, "/team/"+f.tokenA+p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s after the end answered %d, want 404", p, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), "expired") {
			t.Errorf("GET %s after the end doesn't say a link may have expired:\n%s", p, rec.Body.String())
		}
	}
	if rec := switchPosts(t, f, f.tokenA, "off", acctA2); rec.Code != http.StatusNotFound {
		t.Errorf("a switch after the end answered %d, want 404", rec.Code)
	}
	if logins := f.magpie.loginsTaken(); len(logins) != 0 {
		t.Errorf("magpie was told %q after the link ended", logins)
	}
}

// TestThePageSaysWhenItsLinkEnds: a lead whose link has an end is told
// when, and one whose link has none is told nothing new.
func TestThePageSaysWhenItsLinkEnds(t *testing.T) {
	f := newFixture(t)
	expiring(t, f, linkEnds.Add(-24*time.Hour))
	want := linkEnds.Local().Format("2006-01-02 15:04")
	body := pageText(get(t, f.srv, "/team/"+f.tokenA).Body.String())
	if !strings.Contains(body, "This link stops working on "+want) {
		t.Errorf("the page doesn't say the link ends %s:\n%s", want, body)
	}
	if body := pageText(get(t, f.srv, "/team/"+f.tokenB).Body.String()); strings.Contains(body, "This link stops working") {
		t.Errorf("a link with no end says it stops:\n%s", body)
	}
	rec := accepting(t, f, "/team/"+f.tokenA, "zh-CN")
	if !strings.Contains(pageText(rec.Body.String()), "这个链接 "+want+" 之后失效") {
		t.Errorf("the Chinese page doesn't say when the link ends:\n%s", rec.Body.String())
	}
}

// TestTokenGivesTheLinkItsLifetime: a lifetime is counted from ops' clock,
// and none is a link with no end.
func TestTokenGivesTheLinkItsLifetime(t *testing.T) {
	f := newFixture(t)
	if _, team, err := f.ops().Token("team-a", 30*24*time.Hour); err != nil {
		t.Fatal(err)
	} else if want := opsClock.Add(30 * 24 * time.Hour); !team.ExpiresAt.Equal(want) {
		t.Errorf("the link ends %v, want %v", team.ExpiresAt, want)
	}
	for _, onDisk := range f.teamsOnDisk(t) {
		if onDisk.ID == "team-a" && onDisk.ExpiresAt.IsZero() {
			t.Error("teams.json doesn't keep the link's end")
		}
	}
	if _, team, err := f.ops().Token("team-a", 0); err != nil {
		t.Fatal(err)
	} else if !team.ExpiresAt.IsZero() {
		t.Errorf("a link with no lifetime ends %v, want never", team.ExpiresAt)
	}
}
