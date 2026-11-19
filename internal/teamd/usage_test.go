package teamd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// usageAt is the fixture's clock for the usage page, so a test can say what
// "3h ago" is.
func usageAt() time.Time { return time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC) }

// TestUsagePageShowsTheTeamsOwnAccounts: the page's whole job. Its accounts,
// their readings, and the key's two caps; and nothing of another team's.
func TestUsagePageShowsTheTeamsOwnAccounts(t *testing.T) {
	f := newFixture(t)
	rec := get(t, f.srv, "/team/"+f.tokenA+"/usage")
	if rec.Code != http.StatusOK {
		t.Fatalf("the usage page answered %d: %s", rec.Code, rec.Body.String())
	}
	body := pageText(rec.Body.String())
	for _, want := range []string{
		"alice@example.com", "bob@example.com", // the team's accounts
		"5 hours", "7 days", "42%", "100%", // their windows
		"/ 2M", "1.2M", "750k left", // the day cap, what it spent, what is left
		"/ $5.00", "$1.25", "$3.75 left", // the 5-hour cost cap
		"per day", "every 5 hours", // the two windows' words
		"read ",       // when a reading was taken
		"budget read", // and when the key's was
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the usage page doesn't say %q:\n%s", want, body)
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

// TestUsageJSONIsThePageFiltered: the JSON the brief asks for carries the
// same team's figures, and no others.
func TestUsageJSONIsThePageFiltered(t *testing.T) {
	f := newFixture(t)
	rec := get(t, f.srv, "/team/"+f.tokenA+"/usage.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("the usage JSON answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}
	var out usageJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the answer doesn't read: %v\n%s", err, rec.Body.String())
	}
	if out.Team != "team-a" || out.Group != groupA || out.Key != keyA {
		t.Errorf("the answer names %q / %q / %q, want team A's", out.Team, out.Group, out.Key)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("the answer lists %d accounts, want team A's 2: %+v", len(out.Rows), out.Rows)
	}
	for _, r := range out.Rows {
		if strings.EqualFold(r.Entry, acctB1) {
			t.Errorf("another team's account is in the answer: %+v", r)
		}
	}
	// both caps, with what is left of each, as magpie worked it out
	if len(out.Limits) != 2 {
		t.Fatalf("the answer lists %d limits, want 2: %+v", len(out.Limits), out.Limits)
	}
	if l := out.Limits[0]; l.Period != "day" || l.Tokens != 2_000_000 || l.TokensUsed != 1_250_000 || l.TokensLeft != 750_000 {
		t.Errorf("the day cap reads %+v", l)
	}
	if l := out.Limits[1]; l.Period != "hours" || l.Hours != 5 || l.CostLeft != 3.75 {
		t.Errorf("the 5-hour cap reads %+v", l)
	}
	// and every figure is dated
	if out.KeyRead.IsZero() {
		t.Error("the answer doesn't say when the key's budget was read")
	}
	if out.ReadAt == nil {
		t.Error("the answer doesn't say when the allowances were read")
	}
	if strings.Contains(rec.Body.String(), "carol@example.com") {
		t.Errorf("another team's account is in the payload: %s", rec.Body.String())
	}
}

// TestAnAccountWithNoReadingIsNotDrawnAsZero: an account magpie has never
// read is said, never drawn as a 0% one.
func TestAnAccountWithNoReadingIsNotDrawnAsZero(t *testing.T) {
	f := newFixture(t)
	// carol has no reading at all, and is put in team A's group for a look
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, acctB1}},
		{ID: groupB, Name: "Team B"},
	})
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "magpie has no reading of this account yet") {
		t.Errorf("the page doesn't say carol was never read:\n%s", body)
	}
	if !strings.Contains(body, "not read") {
		t.Errorf("the row isn't chipped as unread:\n%s", body)
	}
	// and it is not drawn as an account at 0%
	if strings.Contains(body, "0% of its tightest window") {
		t.Errorf("an account magpie never read is drawn at 0%%:\n%s", body)
	}
}

// TestTheKeyWithNoCapSaysSo: a key with no limit has no window to draw, and
// the page says that rather than showing an empty table.
func TestTheKeyWithNoCapSaysSo(t *testing.T) {
	f := newFixture(t)
	// team B's key has no cap
	body := pageText(get(t, f.srv, "/team/"+f.tokenB+"/usage").Body.String())
	if !strings.Contains(body, "No cap") {
		t.Errorf("the page doesn't say the key has no cap:\n%s", body)
	}
	if strings.Contains(body, "spent") {
		t.Errorf("a key with no cap is drawn as having spent one:\n%s", body)
	}
}

// TestALimitWithSeveralWindowsIsDrawnWhole: a key held to two caps is held
// by whichever runs out first, and the page draws both.
func TestALimitWithSeveralWindowsIsDrawnWhole(t *testing.T) {
	f := newFixture(t)
	// the 5-hour cap is spent; the day one has room
	f.magpie.mu.Lock()
	for i := range f.magpie.keys {
		if f.magpie.keys[i].ID == keyA {
			f.magpie.keys[i].UsedAll[1].Spent = true
			f.magpie.keys[i].UsedAll[1].CostLeft = 0
			f.magpie.keys[i].UsedAll[1].Cost = 5
		}
	}
	f.magpie.mu.Unlock()
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	for _, want := range []string{"spent", "within its cap", "per day", "every 5 hours", "$0 left", "750k left"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't say %q:\n%s", want, body)
		}
	}
}

// TestALimitWhoseResetIsUnknownSaysSo: an account limit follows a window
// magpie may not have read, and a zero time is not "resets now".
func TestALimitWhoseResetIsUnknownSaysSo(t *testing.T) {
	f := newFixture(t)
	// magpie's word is what decides: an account limit says its reset isn't
	// known, and a time that came with it is not printed as one
	f.magpie.mu.Lock()
	for i := range f.magpie.keys {
		if f.magpie.keys[i].ID == keyA {
			f.magpie.keys[i].Limit = &Limit{Period: "account", Account: "claude/alice@example.com", AccountWindow: "5h"}
			f.magpie.keys[i].MoreLimits = nil
			f.magpie.keys[i].UsedAll = []LimitUse{{
				Period: "account", Calls: 3, Tokens: 100, TokenLimit: 1000, TokensLeft: 900,
				ResetUnknown: true,
				Reset:        time.Date(2027, 3, 4, 5, 6, 0, 0, time.UTC),
			}}
		}
	}
	f.magpie.mu.Unlock()
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "magpie can't say when this one starts again") {
		t.Errorf("the page doesn't say the reset is unknown:\n%s", body)
	}
	if strings.Contains(body, "03-04 05:06") || strings.Contains(body, "2027") {
		t.Errorf("a reset magpie says it doesn't know is printed as a time:\n%s", body)
	}
	// and a zero reset magpie didn't flag is unknown too: a zero time is not
	// "resets now", and printing it would say 1970
	f.magpie.mu.Lock()
	for i := range f.magpie.keys {
		if f.magpie.keys[i].ID == keyA {
			f.magpie.keys[i].UsedAll = []LimitUse{{
				Period: "account", Calls: 3, Tokens: 100, TokenLimit: 1000, TokensLeft: 900,
			}}
		}
	}
	f.magpie.mu.Unlock()
	body = pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "magpie can't say when this one starts again") {
		t.Errorf("a zero reset isn't read as unknown:\n%s", body)
	}
	// and it never prints a 1970 date or "resets now"
	for _, bad := range []string{"01-01 00:00", "1970", "resets now"} {
		if strings.Contains(body, bad) {
			t.Errorf("an unknown reset is drawn as %q:\n%s", bad, body)
		}
	}
	// the window's own name is used, so the lead can find it in magpie
	if !strings.Contains(body, "5h of claude/alice@example.com") {
		t.Errorf("the page doesn't name the account's window:\n%s", body)
	}
}

// TestTheUsagePageIsTheTeamsOwn is the boundary: another team's token reads
// its own figures, and no one's token reads nothing.
func TestTheUsagePageIsTheTeamsOwn(t *testing.T) {
	f := newFixture(t)
	body := pageText(get(t, f.srv, "/team/"+f.tokenB+"/usage").Body.String())
	if !strings.Contains(body, "carol@example.com") {
		t.Errorf("team B's page doesn't show its own account:\n%s", body)
	}
	for _, leak := range []string{"alice@example.com", "bob@example.com", acctA1, groupA, keyA, "team-a"} {
		if strings.Contains(body, leak) {
			t.Errorf("team B's page names %q, which is team A's:\n%s", leak, body)
		}
	}
	// team B's own key is the one whose budget is drawn
	if !strings.Contains(body, keyB) {
		t.Errorf("team B's page doesn't name its own key:\n%s", body)
	}
	// and its key has no cap, where team A's has two: the figures aren't
	// another team's
	if !strings.Contains(body, "No cap") {
		t.Errorf("team B's page doesn't show its own key's budget:\n%s", body)
	}
	// an unknown token reads nothing at all
	for _, path := range []string{"/team/nope/usage", "/team/nope/usage.json"} {
		rec := get(t, f.srv, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
}

// TestTheKeysFiguresAreMagpiesNotThePages: what is left of a cap is
// magpie's own arithmetic. A page that recomputed it from the limit and the
// spend would disagree with magpie whenever the two are worked out against
// different windows — which is exactly an "account" limit.
func TestTheKeysFiguresAreMagpiesNotThePages(t *testing.T) {
	f := newFixture(t)
	// magpie says 900 left of a 1000 cap it counted 100 against: consistent
	// here, then made deliberately inconsistent, as an account limit is
	f.magpie.mu.Lock()
	for i := range f.magpie.keys {
		if f.magpie.keys[i].ID == keyA {
			f.magpie.keys[i].Limit = &Limit{Period: "day", Tokens: 1000}
			f.magpie.keys[i].MoreLimits = nil
			// 100 counted, but only 400 left: the key's window is not the
			// one the limit's own window would give
			f.magpie.keys[i].UsedAll = []LimitUse{{
				Period: "day", Tokens: 100, TokenLimit: 1000, TokensLeft: 400,
				Reset: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC),
			}}
		}
	}
	f.magpie.mu.Unlock()
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "400 left") {
		t.Errorf("the page doesn't carry magpie's own \"what is left\":\n%s", body)
	}
	if strings.Contains(body, "900 left") {
		t.Errorf("the page worked out what is left itself:\n%s", body)
	}
}

// TestTheUsagePageSaysWhenAReadingFailed: one read failing doesn't hide the
// other, and neither is drawn as a figure.
func TestTheUsagePageSaysWhenAReadingFailed(t *testing.T) {
	f := newFixture(t)
	f.magpie.mu.Lock()
	f.magpie.status, f.magpie.body = 500, `{"error":"logins.json can't be read: claude/carol@example.com"}`
	f.magpie.mu.Unlock()
	// the boundary reads the group first, so an unreadable magpie is 503
	rec := get(t, f.srv, "/team/"+f.tokenA+"/usage")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable magpie answered %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "carol@example.com") {
		t.Errorf("magpie's own error reached the page: %s", rec.Body.String())
	}
}

// TestTheUsagePageSaysHowOldItsFiguresAre: every number is dated, and a
// reading kept from before says so rather than looking live.
func TestTheUsagePageSaysHowOldItsFiguresAre(t *testing.T) {
	f := newFixture(t)
	f.srv.now = usageAt
	// bob's reading is one magpie kept: the vendor couldn't be asked
	f.magpie.mu.Lock()
	f.magpie.usage = claudeUsage()
	f.magpie.usage["bob@example.com"] = map[string]any{
		"windows": []map[string]any{{"name": "5 hours", "used": 7.0}},
		"readAt":  time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC),
		"asOf":    time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC),
	}
	f.magpie.mu.Unlock()
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if !strings.Contains(body, "read 1h ago") {
		t.Errorf("the page doesn't date its readings:\n%s", body)
	}
	if !strings.Contains(body, "the vendor couldn't be asked") {
		t.Errorf("a kept reading isn't marked as one:\n%s", body)
	}
	if !strings.Contains(body, "budget read") {
		t.Errorf("the key's budget isn't dated:\n%s", body)
	}
}

// TestTheUsagePageLeaksNothingToAnotherOrigin: it carries no script, loads
// nothing, and may not be framed — as every page here.
func TestTheUsagePageLeaksNothingToAnotherOrigin(t *testing.T) {
	f := newFixture(t)
	rec := get(t, f.srv, "/team/"+f.tokenA+"/usage")
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the page's CSP is %q, which doesn't say %q", csp, want)
		}
	}
	body := rec.Body.String()
	for _, bad := range []string{"<script", "src=", "http://", "https://"} {
		if strings.Contains(body, bad) {
			t.Errorf("the page holds %q:\n%s", bad, body)
		}
	}
}

// TestTheNumbersThePagePrints: the page's own arithmetic, at the boundaries
// where a count would otherwise be said in two units for one number.
func TestTheNumbersThePagePrints(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{0, "0"}, {-5, "0"}, {999, "999"}, {1000, "1k"},
		{999_949, "999.9k"},
		// 999,950 rounds to 1000k, which is one number in two units: it is
		// said as 1M
		{999_950, "1M"}, {999_999, "1M"}, {1_000_000, "1M"},
		{1_250_000, "1.2M"}, {2_000_000, "2M"},
		{999_949_999, "999.9M"}, {999_950_000, "1B"}, {1_500_000_000, "1.5B"},
	} {
		if got := fmtTokens(c.n); got != c.want {
			t.Errorf("fmtTokens(%d) is %q, want %q", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		f    float64
		want string
	}{{0, "$0"}, {0.005, "$0.01"}, {1.25, "$1.25"}, {5, "$5.00"}, {123.456, "$123.46"}} {
		if got := fmtCost(c.f); got != c.want {
			t.Errorf("fmtCost(%v) is %q, want %q", c.f, got, c.want)
		}
	}
	// a share never leaves 0 to 100, whatever the arithmetic says
	for _, c := range []struct {
		used, limit float64
		want        int
	}{{0, 0, 0}, {5, 0, 0}, {-1, 10, 0}, {0, 10, 0}, {5, 10, 50}, {10, 10, 100}, {15, 10, 100}} {
		if got := shareInt(c.used, c.limit); got != c.want {
			t.Errorf("shareInt(%v, %v) is %d, want %d", c.used, c.limit, got, c.want)
		}
	}
}

// TestARowWithNoSpendSaysWhichKind: a lost entry and an API key have no
// spend of their own, and each says which it is rather than being drawn as
// an account magpie happens not to have read.
func TestARowWithNoSpendSaysWhichKind(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, "claude/9999999999999999", "claude/ffffffffffffffff"}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	key := sectionOf(body, "claude/9999999999999999")
	if !strings.Contains(key, "API key") {
		t.Errorf("the key's row isn't chipped as one:\n%s", key)
	}
	lost := sectionOf(body, "claude/ffffffffffffffff")
	if !strings.Contains(lost, "not in magpie") {
		t.Errorf("the lost entry isn't chipped as one:\n%s", lost)
	}
	// and neither is drawn as an account nobody read
	for _, row := range []string{key, lost} {
		if strings.Contains(row, "not read") {
			t.Errorf("a row with nothing to read is drawn as unread:\n%s", row)
		}
	}
}

// TestAKeptReadingIsSaidOnce: a reading magpie kept because the vendor
// couldn't be asked is marked as one, once. Said in the read line and again
// in the note, it reads as two faults.
func TestAKeptReadingIsSaidOnce(t *testing.T) {
	f := newFixture(t)
	f.srv.now = usageAt
	f.magpie.mu.Lock()
	f.magpie.usage = claudeUsage()
	f.magpie.usage["bob@example.com"] = map[string]any{
		"windows": []map[string]any{{"name": "5 hours", "used": 7.0}},
		"readAt":  time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC),
		"asOf":    time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC),
	}
	f.magpie.mu.Unlock()
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/usage").Body.String())
	if n := strings.Count(sectionOf(body, "bob@example.com"), "the vendor couldn't be asked"); n != 1 {
		t.Errorf("bob's card says the vendor couldn't be asked %d times, want once:\n%s", n, sectionOf(body, "bob@example.com"))
	}
}
