package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/usage"
)

// issuerUsageOf reads the route's answer for one issuer and period.
func issuerUsageOf(t *testing.T, query string) (issuerUsageAnswer, *httptest.ResponseRecorder) {
	t.Helper()
	w := adminDo(t, "GET", "/gateway-keys/issuer-usage?"+query, "")
	if w.Code != 200 {
		t.Fatalf("issuer-usage?%s: %d %s", query, w.Code, w.Body)
	}
	var out issuerUsageAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return out, w
}

// spendFor is the row of one key in an answer, false when it has none.
func spendFor(a issuerUsageAnswer, keyID string) (issuerSpend, bool) {
	for _, k := range a.Keys {
		if k.KeyID == keyID {
			return k, true
		}
	}
	return issuerSpend{}, false
}

// A team portal reads what each member's key spent through one read-only
// route, filtered by the key they were all issued under: the ledger keeps
// the issuer on every record, so a key revoked since still counts.
func TestAdminIssuerUsageCountsOnlyItsOwnKeys(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	other := issueUnder(t, "team-b-key", "claude/*")
	alice, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Alice","ttlSeconds":604800}`)
	bob, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Bob","ttlSeconds":604800}`)
	carol, _ := issuedFromAdmin(t, `{"issuer":"`+other+`","name":"Carol","ttlSeconds":604800}`)
	at := time.Now().Add(-time.Hour)
	call := usage.Record{Time: at, Agent: "claude", Provider: "claude", Model: "claude-sonnet-5-5", Status: 200, Input: 100, Output: 10}
	with := func(k access.Issued) usage.Record {
		r := call
		r.CallerKeyID, r.CallerKeyName, r.CallerIssuerID = k.ID, k.Name, team
		return r
	}
	usage.Append(with(alice))
	usage.Append(with(alice))
	usage.Append(with(bob))
	foreign := call
	foreign.CallerKeyID, foreign.CallerKeyName, foreign.CallerIssuerID = carol.ID, carol.Name, other
	usage.Append(foreign)
	// a record from before the issuer was kept belongs to no issuer at all
	old := call
	old.CallerKeyID, old.CallerKeyName = alice.ID, alice.Name
	usage.Append(old)

	answer, _ := issuerUsageOf(t, "issuer="+team+"&period=today")
	if len(answer.Keys) != 2 {
		t.Fatalf("the answer holds %d keys, want Alice and Bob: %+v", len(answer.Keys), answer.Keys)
	}
	a, ok := spendFor(answer, alice.ID)
	if !ok || a.Name != "Alice" || a.Calls != 2 || a.Input != 200 || a.Output != 20 {
		t.Fatalf("Alice's row is %+v (found %v)", a, ok)
	}
	b, ok := spendFor(answer, bob.ID)
	if !ok || b.Name != "Bob" || b.Calls != 1 || b.Input != 100 {
		t.Fatalf("Bob's row is %+v (found %v)", b, ok)
	}
	// another team's key is in the same ledger and must not reach this answer
	if _, ok := spendFor(answer, carol.ID); ok {
		t.Fatalf("another issuer's key is in the answer: %+v", answer.Keys)
	}
	if answer.Total.Calls != 3 || answer.Total.Input != 300 || answer.Total.Output != 30 {
		t.Fatalf("the issuer's total is %+v, want 3 calls and 300 in", answer.Total)
	}
	// the keys that used most first
	if answer.Keys[0].KeyID != alice.ID {
		t.Fatalf("the ranking starts with %s, want Alice", answer.Keys[0].Name)
	}

	// the other team asks, and sees only its own
	otherAnswer, _ := issuerUsageOf(t, "issuer="+other+"&period=today")
	if len(otherAnswer.Keys) != 1 || otherAnswer.Keys[0].KeyID != carol.ID {
		t.Fatalf("the other team's answer is %+v", otherAnswer.Keys)
	}
}

// A member's key revoked since is gone from magpie's list and still in the
// ledger: its row keeps the name the record carried, and its numbers.
func TestAdminIssuerUsageKeepsARevokedKeysHistory(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	gone, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Departed","ttlSeconds":604800}`)
	usage.Append(usage.Record{
		Time: time.Now().Add(-time.Hour), Agent: "claude", Provider: "claude", Model: "claude-sonnet-5-5",
		Status: 200, Input: 500, Output: 50, CallerKeyID: gone.ID, CallerKeyName: "Departed", CallerIssuerID: team,
	})
	if w := adminDo(t, "POST", "/gateway-keys/revoke-key", `{"issuer":"`+team+`","id":"`+gone.ID+`"}`); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if _, ok := access.Authenticate(gone.Key); ok {
		t.Fatal("the key wasn't revoked")
	}
	answer, _ := issuerUsageOf(t, "issuer="+team+"&period=today")
	row, ok := spendFor(answer, gone.ID)
	if !ok {
		t.Fatalf("a revoked key's history is gone from the answer: %+v", answer.Keys)
	}
	if row.Name != "Departed" || row.Calls != 1 || row.Input != 500 {
		t.Fatalf("the revoked key's row is %+v", row)
	}
}

// Each period answers the window it names, and a window with nothing in it
// is an empty list rather than an error.
func TestAdminIssuerUsageNamesItsWindow(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	now := time.Now()
	for _, c := range []struct {
		period string
		want   time.Time
	}{
		{"today", usage.Today.Since(now)},
		{"7d", usage.Week.Since(now)},
		{"30d", usage.Month.Since(now)},
	} {
		answer, _ := issuerUsageOf(t, "issuer="+team+"&period="+c.period)
		if answer.Period != c.period {
			t.Errorf("%s: the answer says period %q", c.period, answer.Period)
		}
		// the two sides are worked out a moment apart, so a second either
		// way is the resolution of the comparison, not of the window
		if d := answer.Since.Sub(c.want); d > time.Second || d < -time.Second {
			t.Errorf("%s: since is %s, want %s", c.period, answer.Since, c.want)
		}
		if answer.Keys == nil {
			t.Errorf("%s: the answer holds no keys list", c.period)
		}
		if answer.Total.Calls != 0 {
			t.Errorf("%s: a team with no calls has %d", c.period, answer.Total.Calls)
		}
	}
}

// A missing issuer, or a period this route doesn't have, is a 400 that says
// what it wanted.
func TestAdminIssuerUsageRefusesWhatItCannotAnswer(t *testing.T) {
	sandboxHome(t)
	for _, c := range []struct{ query, want string }{
		{"period=today", "issuer"},
		{"issuer=abc", "period"},
		{"issuer=abc&period=all", "period"},
		{"issuer=abc&period=year", "period"},
		{"issuer=&period=today", "issuer"},
	} {
		w := adminDo(t, "GET", "/gateway-keys/issuer-usage?"+c.query, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400 (%s)", c.query, w.Code, w.Body)
			continue
		}
		if !containsFold(w.Body.String(), c.want) {
			t.Errorf("%s: the refusal doesn't name %q: %s", c.query, c.want, w.Body)
		}
	}
}

// Nothing in the answer is a secret: a key's masked form is not even that,
// and no row carries one.
func TestAdminIssuerUsageTellsNoSecret(t *testing.T) {
	sandboxHome(t)
	team := issueUnder(t, "team-a-key", "claude/*")
	alice, _ := issuedFromAdmin(t, `{"issuer":"`+team+`","name":"Alice","ttlSeconds":604800}`)
	usage.Append(usage.Record{
		Time: time.Now().Add(-time.Minute), Agent: "claude", Provider: "claude", Model: "claude-sonnet-5-5",
		Status: 200, Input: 10, Output: 1, CallerKeyID: alice.ID, CallerKeyName: "Alice", CallerIssuerID: team,
	})
	_, resp := issuerUsageOf(t, "issuer="+team+"&period=today")
	body := resp.Body.String()
	var raw map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["secret"]; ok {
		t.Fatalf("the answer carries a secret field: %s", body)
	}
	if containsFold(body, alice.Key) {
		t.Fatal("the answer carries a key's secret")
	}
}

// containsFold is a refusal's wording checked without its case.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
