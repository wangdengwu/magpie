package gui

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/teamcore"
)

// opsPageAndStore is opsPage with the store it reads kept, so a test can
// put a link past its end.
func opsPageAndStore(t *testing.T) (http.Handler, *teamcore.Store) {
	t.Helper()
	sandboxHome(t)
	store, err := teamcore.NewStore(filepath.Join(t.TempDir(), "teamd"), 0)
	if err != nil {
		t.Fatal(err)
	}
	return opsRoutes(store, guiMagpie{}), store
}

// linkAnswer reads a write's answer as the page does.
func linkAnswer(t *testing.T, body []byte) opsLink {
	t.Helper()
	var out opsLink
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err, string(body))
	}
	return out
}

// endsIn says at is d from when the request was sent, give or take the
// time the request took.
func endsIn(t *testing.T, what string, at, before time.Time, d time.Duration) {
	t.Helper()
	if lo, hi := before.Add(d), time.Now().Add(d); at.Before(lo) || at.After(hi) {
		t.Errorf("%s ends %v, want %v from the request (%v to %v)", what, at, d, lo, hi)
	}
}

// TestOpsPageGivesALinkTheLifetimeItIsAsked: add and New link each take
// the lifetime the page picked, the answer says when the link ends, and
// the listing says so after.
func TestOpsPageGivesALinkTheLifetimeItIsAsked(t *testing.T) {
	h, _ := opsPageAndStore(t)
	g, keyID := opsGroupAndKey(t)

	before := time.Now()
	rec := opsDo(t, h, "POST", "/ops/teams/add", opsAsk{Name: "team-a", Group: g.ID, Key: keyID, TTL: "30d"})
	if rec.Code != http.StatusOK {
		t.Fatalf("adding with 30d answered %d: %s", rec.Code, rec.Body)
	}
	endsIn(t, "the made link", linkAnswer(t, rec.Body.Bytes()).ExpiresAt, before, 30*24*time.Hour)
	team := opsListed(t, h).Teams[0]
	endsIn(t, "the listed link", team.ExpiresAt, before, 30*24*time.Hour)
	if team.Expired || !team.Linked || team.Lifetime != "30d" {
		t.Errorf("a link 30 days from its end is listed as %+v", team)
	}

	before = time.Now()
	rec = opsDo(t, h, "POST", "/ops/teams/token", opsAsk{ID: "team-a", TTL: "7d"})
	if rec.Code != http.StatusOK {
		t.Fatalf("New link with 7d answered %d: %s", rec.Code, rec.Body)
	}
	endsIn(t, "the new link", linkAnswer(t, rec.Body.Bytes()).ExpiresAt, before, 7*24*time.Hour)

	// forever, and a page that sends no lifetime at all, give a link no end
	for _, ttl := range []string{"forever", ""} {
		rec = opsDo(t, h, "POST", "/ops/teams/token", opsAsk{ID: "team-a", TTL: ttl})
		if rec.Code != http.StatusOK {
			t.Fatalf("New link with %q answered %d: %s", ttl, rec.Code, rec.Body)
		}
		if at := linkAnswer(t, rec.Body.Bytes()).ExpiresAt; !at.IsZero() {
			t.Errorf("New link with %q ends %v, want never", ttl, at)
		}
		if team := opsListed(t, h).Teams[0]; !team.ExpiresAt.IsZero() || team.Lifetime != "forever" {
			t.Errorf("after New link with %q the link is listed as %+v", ttl, team)
		}
	}
}

// TestOpsPageRefusesALifetimeItDoesNotOffer: a lifetime the page doesn't
// offer is refused before anything is made or replaced.
func TestOpsPageRefusesALifetimeItDoesNotOffer(t *testing.T) {
	h, _ := opsPageAndStore(t)
	g, keyID := opsGroupAndKey(t)

	rec := opsDo(t, h, "POST", "/ops/teams/add", opsAsk{Name: "team-a", Group: g.ID, Key: keyID, TTL: "1y"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("adding with 1y answered %d: %s", rec.Code, rec.Body)
	}
	if n := len(opsListed(t, h).Teams); n != 0 {
		t.Fatalf("a refused lifetime made %d teams", n)
	}
	// and the key it would have held is left as it was
	if got := keyAccounts(t, keyID); len(got) != 0 {
		t.Errorf("a refused lifetime held the key: %v", got)
	}

	opsAdd(t, h, "team-a", g.ID, keyID)
	rec = opsDo(t, h, "POST", "/ops/teams/token", opsAsk{ID: "team-a", TTL: "nonsense"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("New link with nonsense answered %d: %s", rec.Code, rec.Body)
	}
	var e opsErr
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error != "Pick one of the lifetimes the page offers." {
		t.Errorf("the refusal reads %q (%v), want the page's own words", e.Error, err)
	}
}

// TestOpsPageListsALinkPastItsEnd: a link past its end is listed as such,
// so the page says so instead of drawing it as a link that works.
func TestOpsPageListsALinkPastItsEnd(t *testing.T) {
	h, store := opsPageAndStore(t)
	g, keyID := opsGroupAndKey(t)
	opsAdd(t, h, "team-a", g.ID, keyID)
	ended := time.Now().Add(-time.Hour).UTC()
	if err := store.Update(func(teams []teamcore.Team) ([]teamcore.Team, error) {
		teams[0].ExpiresAt = ended
		return teams, nil
	}); err != nil {
		t.Fatal(err)
	}
	team := opsListed(t, h).Teams[0]
	if !team.Expired || !team.ExpiresAt.Equal(ended) {
		t.Errorf("a link an hour past its end is listed as %+v", team)
	}
}
