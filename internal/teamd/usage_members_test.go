package teamd

import (
	"net/http"
	"strings"
	"testing"
)

// memberSpendAt is the fixture's answer to what each member key used, over
// the three periods the page shows: Alice used a lot this week, Bob a
// little today, and Departed used some before the team revoked their key.
func memberSpends(f *fixture) {
	f.magpie.setSpend(keyA, "today",
		IssuerSpend{KeyID: "member1", Name: "Alice", Calls: 4, Input: 400, Output: 40, Cost: 0.4},
		IssuerSpend{KeyID: "member2", Name: "Bob", Calls: 1, Input: 50, Output: 5, Cost: 0.05},
	)
	f.magpie.setSpend(keyA, "7d",
		IssuerSpend{KeyID: "member1", Name: "Alice", Calls: 40, Input: 4000, Output: 400, Cost: 4},
		IssuerSpend{KeyID: "member3", Name: "Departed", Calls: 9, Input: 900, Output: 90, Cost: 0.9},
		IssuerSpend{KeyID: "member2", Name: "Bob", Calls: 2, Input: 100, Output: 10, Cost: 0.1},
	)
	f.magpie.setSpend(keyA, "30d",
		IssuerSpend{KeyID: "member1", Name: "Alice", Calls: 400, Input: 40000, Output: 4000, Cost: 40},
		IssuerSpend{KeyID: "member3", Name: "Departed", Calls: 9, Input: 900, Output: 90, Cost: 0.9},
	)
}

// issueMembers makes the fixture's magpie hold three member keys under team
// A's gateway key, the third of them revoked since.
func issueMembers(t *testing.T, f *fixture) {
	t.Helper()
	ctx := t.Context()
	for _, name := range []string{"Alice", "Bob", "Departed"} {
		if _, err := f.srv.admin.IssueMemberKey(ctx, keyA, name, MaxMemberTTL); err != nil {
			t.Fatal(err)
		}
	}
	// a revoked key is gone from the live list and still in the ledger
	if err := f.srv.admin.RevokeMemberKey(ctx, keyA, "member3"); err != nil {
		t.Fatal(err)
	}
}

// TestUsagePageShowsWhatEachMemberKeyUsed is the block's whole job: one row
// per member key over the three periods, the member who used most first,
// and a revoked key still counted.
func TestUsagePageShowsWhatEachMemberKeyUsed(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	memberSpends(f)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	for _, want := range []string{
		"What each member key has used",
		"Today", "Last 7 days", "Last 30 days",
		"Alice", "Bob", "Departed",
		"4 · 440 tokens · $0.40",    // Alice today
		"40 · 4.4k tokens · $4.00",  // Alice this week
		"400 · 44k tokens · $40.00", // Alice this month
		"revoked",                   // Departed's key is gone from the live list
		"9 · 990 tokens · $0.90",    // and its figures are still there
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't say %q:\n%s", want, body)
		}
	}
	// the member who used most over the week first
	if i, j := strings.Index(body, "Alice"), strings.Index(body, "Departed"); i < 0 || j < 0 || i > j {
		t.Errorf("the rows aren't ranked by the week's tokens:\n%s", body)
	}
	// and nothing of another team's keys
	if strings.Contains(body, "Carol") {
		t.Errorf("the page names another team's key:\n%s", body)
	}
}

// TestAMemberWithNoCallsShowsADash: a key nobody used in a period is a dash,
// not a zero, and one used in some periods only shows figures in those.
func TestAMemberWithNoCallsShowsADash(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	// Bob used nothing this week or this month, and Alice nothing today
	f.magpie.setSpend(keyA, "today",
		IssuerSpend{KeyID: "member1", Name: "Alice", Calls: 1, Input: 10, Output: 1, Cost: 0.01},
	)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "—") {
		t.Errorf("a member with no calls in a period isn't drawn as a dash:\n%s", body)
	}
	for _, zero := range []string{"0 · 0 tokens", "0 tokens"} {
		if strings.Contains(body, zero) {
			t.Errorf("an empty period is drawn as %q:\n%s", zero, body)
		}
	}
}

// TestNoMemberKeysSaysWhereToIssueOne: a team that has issued none gets an
// explanation and a way there, not an empty table.
func TestNoMemberKeysSaysWhereToIssueOne(t *testing.T) {
	f := newFixture(t)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "No member keys yet") {
		t.Errorf("the page draws no explanation for a team with no member keys:\n%s", body)
	}
	if !strings.Contains(body, "/team/"+f.tokenA+"/members") {
		t.Errorf("the page doesn't point at the members page:\n%s", body)
	}
}

// TestAFailedMemberReadSaysSo: a period whose read failed says the figures
// couldn't be read, and draws no totals. A failed read is unknown, never
// empty.
func TestAFailedMemberReadSaysSo(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	f.magpie.mu.Lock()
	f.magpie.refuseSpend = map[string]int{"7d": http.StatusInternalServerError}
	f.magpie.mu.Unlock()
	f.magpie.setSpend(keyA, "today",
		IssuerSpend{KeyID: "member1", Name: "Alice", Calls: 1, Input: 10, Output: 1, Cost: 0.01},
	)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "couldn't be asked what the member keys used") {
		t.Errorf("the page doesn't say the member figures couldn't be read:\n%s", body)
	}
	// the period that failed is named
	if !strings.Contains(body, "Last 7 days") {
		t.Errorf("the page doesn't name the period that failed:\n%s", body)
	}
	// the period that worked is still drawn
	if !strings.Contains(body, "1 · 11 tokens") {
		t.Errorf("one failed read blanked the periods that worked:\n%s", body)
	}
	// and the failed one is a dash, never a zero
	if strings.Contains(body, "0 · 0 tokens") {
		t.Errorf("the period that couldn't be read is drawn as zeros:\n%s", body)
	}
}

// TestTheIssuerIsTheTeamsOwn: the page asks magpie about the team record's
// key, whatever a request says. A query naming another key changes nothing.
func TestTheIssuerIsTheTeamsOwn(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	memberSpends(f)
	// team B's own key is asked about, and answers with nothing: the page
	// must still show team A's
	f.magpie.setSpend(keyB, "7d", IssuerSpend{KeyID: "member9", Name: "Somebody Else", Calls: 7, Input: 70})
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage?issuer="+keyB).Body.String())
	if strings.Contains(body, "Somebody Else") {
		t.Errorf("a request's issuer reached the page:\n%s", body)
	}
	if !strings.Contains(body, "Alice") {
		t.Errorf("the page doesn't show the team's own keys:\n%s", body)
	}
}

// TestTheMemberBlockTellsNoSecret: the figures are counts, and no key's
// secret is anywhere on the page.
func TestTheMemberBlockTellsNoSecret(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	memberSpends(f)
	body := get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String()
	for _, secret := range []string{f.magpie.lastIssuedKey(), "sk-magpie-key-issued"} {
		if secret != "" && strings.Contains(body, secret) {
			t.Errorf("the page carries a key's secret:\n%s", body)
		}
	}
}
