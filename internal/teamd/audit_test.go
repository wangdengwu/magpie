package teamd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// auditEntries is the fixture's audit file, read back.
func auditEntries(t *testing.T, f *fixture) []Entry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.srv.Store().Dir(), auditFile))
	if err != nil {
		t.Fatalf("the audit file can't be read: %v", err)
	}
	var out []Entry
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("an audit line doesn't read: %v\n%s", err, line)
		}
		out = append(out, e)
	}
	return out
}

// TestEachChangeLeavesOneLine: adding, disabling and enabling each write
// exactly one line, saying what it was and which account.
func TestEachChangeLeavesOneLine(t *testing.T) {
	f := newFixture(t)
	// add an account through the sign-in flow
	st := startSignIn(t, f)
	if rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor("aud1")}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("the paste answered %d: %s", rec.Code, rec.Body.String())
	}
	// and disable, then enable, one of the team's own
	if rec := switchPosts(t, f, f.tokenA, "off", acctA2); rec.Code != http.StatusOK {
		t.Fatalf("disabling answered %d: %s", rec.Code, rec.Body.String())
	}
	if rec := switchPosts(t, f, f.tokenA, "on", acctA2); rec.Code != http.StatusOK {
		t.Fatalf("enabling answered %d: %s", rec.Code, rec.Body.String())
	}

	entries := auditEntries(t, f)
	if len(entries) != 3 {
		t.Fatalf("the audit file holds %d lines, want 3: %+v", len(entries), entries)
	}
	for i, want := range []struct{ action, account string }{
		{actAdd, "claude/" + userID("new@example.com")},
		{actOff, acctA2},
		{actOn, acctA2},
	} {
		e := entries[i]
		if e.Action != want.action {
			t.Errorf("line %d says %q, want %q", i+1, e.Action, want.action)
		}
		if !strings.EqualFold(e.Account, want.account) {
			t.Errorf("line %d names %q, want %q", i+1, e.Account, want.account)
		}
		if e.Team != "team-a" {
			t.Errorf("line %d names team %q, want team-a", i+1, e.Team)
		}
		if e.Result != ResultOK {
			t.Errorf("line %d says %q, want ok", i+1, e.Result)
		}
		if e.At.IsZero() {
			t.Errorf("line %d isn't dated", i+1)
		}
	}
	// the added account's line carries the name it is shown by
	if entries[0].Name != "new@example.com" {
		t.Errorf("the added line's name is %q, want the account's", entries[0].Name)
	}
}

// TestARefusedSwitchIsRecorded is the line that matters most: a lead's link
// asking for an account that isn't its own.
func TestARefusedSwitchIsRecorded(t *testing.T) {
	f := newFixture(t)
	for _, action := range []string{"off", "on"} {
		if rec := switchPosts(t, f, f.tokenA, action, acctB1); rec.Code != http.StatusForbidden {
			t.Fatalf("a foreign account answered %d, want 403", rec.Code)
		}
	}
	entries := auditEntries(t, f)
	if len(entries) != 2 {
		t.Fatalf("the audit file holds %d lines, want one per refusal: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Result != ResultDenied {
			t.Errorf("a refused switch is recorded as %q, want refused", e.Result)
		}
		if e.Team != "team-a" {
			t.Errorf("the refusal names team %q, want the team whose link asked", e.Team)
		}
		if !strings.EqualFold(e.Account, acctB1) {
			t.Errorf("the refusal names %q, want the account asked for", e.Account)
		}
	}
}

// TestATokenThatNamesNoTeamIsNotRecorded: a guessed link has no team to be
// written under, and the file is not a log of guesses.
func TestATokenThatNamesNoTeamIsNotRecorded(t *testing.T) {
	f := newFixture(t)
	if rec := switchPosts(t, f, "not-a-token", "off", acctA1); rec.Code != http.StatusNotFound {
		t.Fatalf("a guessed token answered %d, want 404", rec.Code)
	}
	if b := f.auditOnDisk(t); strings.TrimSpace(b) != "" {
		t.Errorf("a guessed token wrote an audit line:\n%s", b)
	}
}

// TestNoSecretReachesTheAuditFile: the file is read by a page and by ops,
// and holds no token, no key and nothing a browser pasted.
func TestNoSecretReachesTheAuditFile(t *testing.T) {
	f := newFixture(t)
	// an add, whose paste carries a code
	st := startSignIn(t, f)
	const code = "SECRET-CODE-abcdef0123456789"
	if rec := postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st.ID+"/callback", url.Values{"url": {pasteFor(code)}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("the paste answered %d: %s", rec.Code, rec.Body.String())
	}
	// a refusal, and a switch
	switchPosts(t, f, f.tokenA, "off", acctB1)
	switchPosts(t, f, f.tokenA, "off", acctA2)
	// a cancel
	st2 := startSignIn(t, f)
	postForm(t, f.srv, "/team/"+f.tokenA+"/signin/"+st2.ID+"/cancel", url.Values{})

	raw := f.auditOnDisk(t)
	for _, secret := range []string{
		f.tokenA, f.tokenB, // a team's link
		testAdminKey, // the admin key
		keyText,      // a mounted team key
		code,         // the pasted callback's code
		stateOf(st),  // the sign-in's state
		st.ID,        // the sign-in's id
		st.URL,       // the vendor's link
	} {
		if secret == "" {
			continue
		}
		if strings.Contains(raw, secret) {
			t.Errorf("the audit file holds %q:\n%s", secret, raw)
		}
	}
	// and it does hold the lines it should
	if !strings.Contains(raw, actAdd) || !strings.Contains(raw, actCancel) || !strings.Contains(raw, ResultDenied) {
		t.Errorf("the audit file is missing a line it should have:\n%s", raw)
	}
}

// stateOf is the state a sign-in carries, for the redaction test: it is in
// the vendor's URL and must be in no audit line.
func stateOf(st SignIn) string {
	if _, _, ok := strings.Cut(st.URL, "state="); ok {
		_, s, _ := strings.Cut(st.URL, "state=")
		if i := strings.IndexAny(s, "&#"); i > 0 {
			s = s[:i]
		}
		return s
	}
	return ""
}

// TestTheAuditPageShowsOnlyThisTeamsLines is the page's boundary.
func TestTheAuditPageShowsOnlyThisTeamsLines(t *testing.T) {
	f := newFixture(t)
	// one change by each team
	if rec := switchPosts(t, f, f.tokenA, "off", acctA2); rec.Code != http.StatusOK {
		t.Fatalf("team A's switch answered %d", rec.Code)
	}
	if rec := switchPosts(t, f, f.tokenB, "off", acctB1); rec.Code != http.StatusOK {
		t.Fatalf("team B's switch answered %d: %s", rec.Code, rec.Body.String())
	}

	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/audit").Body.String())
	if !strings.Contains(body, "bob@example.com") {
		t.Errorf("team A's page doesn't show its own change:\n%s", body)
	}
	for _, leak := range []string{"carol@example.com", acctB1, "team-b", "Team B"} {
		if strings.Contains(body, leak) {
			t.Errorf("team A's page names %q, which is team B's:\n%s", leak, body)
		}
	}
	if rec := get(t, f.srv, "/team/nope/audit"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown token answered %d, want 404", rec.Code)
	}
}

// TestTheAuditPageSaysWhenALineWasRefused: a refused line is drawn as one.
func TestTheAuditPageSaysWhenALineWasRefused(t *testing.T) {
	f := newFixture(t)
	switchPosts(t, f, f.tokenA, "off", acctB1)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/audit").Body.String())
	if !strings.Contains(body, "refused") {
		t.Errorf("the page doesn't say a line was refused:\n%s", body)
	}
	if !strings.Contains(body, "disabled an account") {
		t.Errorf("the page doesn't say what was asked:\n%s", body)
	}
}

// TestTheAuditPageWithNoLines: a fresh team's page says so rather than
// drawing an empty table.
func TestTheAuditPageWithNoLines(t *testing.T) {
	f := newFixture(t)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/audit").Body.String())
	if !strings.Contains(body, "Nothing yet") {
		t.Errorf("an empty history doesn't say so:\n%s", body)
	}
}

// TestTheAuditFileRotates: a long-lived portal can't fill the volume, and
// the generation before the last is kept so a page isn't blank the moment
// the file turns over.
func TestTheAuditFileRotates(t *testing.T) {
	f := newFixture(t)
	// a small bound, so a handful of lines rotates it: a store of its own
	// over the portal's folder
	st := storeAt(t, f.srv.Store().Dir(), 400)
	for i := range 8 {
		if err := st.Audit(Entry{
			At:   time.Date(2026, 11, 19, 10, i, 0, 0, time.UTC),
			Team: "team-a", Action: actOff, Account: acctA1, Result: ResultOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	dir := st.Dir()
	fi, err := os.Stat(filepath.Join(dir, auditFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 400 {
		t.Errorf("the audit file is %d bytes, past the 400 it rotates at", fi.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, auditOld)); err != nil {
		t.Errorf("the file rotated but kept no previous generation: %v", err)
	}
	// and what is read is what is left, not a blank
	entries, total, err := st.AuditLines("team-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 || len(entries) != total {
		t.Errorf("after rotating, the read answered %d of %d: %+v", len(entries), total, entries)
	}
	if total >= 8 {
		t.Errorf("the file rotated and still holds all %d lines: the old generation isn't dropped", total)
	}
}

// TestRotationKeepsTheNewestLines: the lines a lead is most likely to want
// are the last ones, so a rotation drops the oldest.
func TestRotationKeepsTheNewestLines(t *testing.T) {
	f := newFixture(t)
	// a small bound, so a handful of lines rotates it: a store of its own
	// over the portal's folder
	st := storeAt(t, f.srv.Store().Dir(), 300)
	for i := range 10 {
		if err := st.Audit(Entry{
			At:   time.Date(2026, 11, 19, 10, i, 0, 0, time.UTC),
			Team: "team-a", Action: actOff, Account: "claude/" + itoa(i), Result: ResultOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _, err := st.AuditLines("team-a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nothing is left after rotating")
	}
	if last := entries[len(entries)-1]; last.Account != "claude/9" {
		t.Errorf("the newest line is %q, want the last one written", last.Account)
	}
	// and no line is repeated: a rotation that copied rather than renamed
	// would show one twice
	seen := map[string]bool{}
	for _, e := range entries {
		k := e.At.String() + "|" + e.Account
		if seen[k] {
			t.Errorf("a line is in the history twice: %s", k)
		}
		seen[k] = true
	}
}

// TestOneLineIsAlwaysWhole: a line written is a line read, whatever the
// rotation did.
func TestOneLineIsAlwaysWhole(t *testing.T) {
	f := newFixture(t)
	// a small bound, so a handful of lines rotates it: a store of its own
	// over the portal's folder
	st := storeAt(t, f.srv.Store().Dir(), 250)
	for i := range 12 {
		if err := st.Audit(Entry{
			At:   time.Date(2026, 11, 19, 10, i, 0, 0, time.UTC),
			Team: "team-a", Action: actOff, Account: acctA1,
			Name: "A name with · and spaces and 中文, which is JSON's job", Result: ResultOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{auditFile, auditOld} {
		b, err := os.ReadFile(filepath.Join(st.Dir(), name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var e Entry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Errorf("%s holds a line that doesn't read: %v\n%s", name, err, line)
			}
		}
	}
}

// TestASkippedLineDoesNotHideTheRest: half a line from a writer that died
// mid-write is not a reason to show nothing.
func TestASkippedLineDoesNotHideTheRest(t *testing.T) {
	f := newFixture(t)
	s := f.srv.Store()
	if err := s.Audit(Entry{Team: "team-a", Action: actOff, Account: acctA1, Result: ResultOK}); err != nil {
		t.Fatal(err)
	}
	// a truncated line, as a killed writer leaves
	path := filepath.Join(s.Dir(), auditFile)
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(good, `{"at":"2026-11-19T10:00:00Z","team":"team-a","act`...), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, total, err := s.AuditLines("team-a", 100)
	if err != nil {
		t.Fatalf("a truncated line failed the read: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Errorf("the read answered %d of %d, want the one whole line", len(entries), total)
	}
}

// TestAuditRefusedWritesNothingForNoTeam is the guard behind the route's:
// a refusal that says the token named no team writes no line, whatever
// calls auditRefused. The route can't show it — teamOf refuses a guessed
// token again a line later — so it is checked where it lives.
func TestAuditRefusedWritesNothingForNoTeam(t *testing.T) {
	f := newFixture(t)
	s := f.srv
	// the two refusals a link can get, and only the second is a team's
	s.auditRefused("not-a-token", actOff, acctA1, ErrNoTeam)
	if b := f.auditOnDisk(t); strings.TrimSpace(b) != "" {
		t.Errorf("a refusal naming no team wrote a line:\n%s", b)
	}
	s.auditRefused(f.tokenA, actOff, acctB1, fmt.Errorf("not this team's: %w", ErrNotYours))
	entries := auditEntries(t, f)
	if len(entries) != 1 || entries[0].Team != "team-a" || entries[0].Result != ResultDenied {
		t.Errorf("a refusal naming a team wrote %+v, want one refused line for team-a", entries)
	}
}

// TestTheHistoryReadsAtNarrowWidths: five columns at 390px leaves one word
// a line. Below the breakpoint each change becomes a block, its labels on
// the left of what they name — which needs the labels in the markup.
func TestTheHistoryReadsAtNarrowWidths(t *testing.T) {
	f := newFixture(t)
	switchPosts(t, f, f.tokenA, "off", acctA2)
	body := get(t, f.srv, "/team/"+f.tokenA+"/audit").Body.String()
	for _, label := range []string{"When", "What", "Account", "By"} {
		if !strings.Contains(body, `data-label="`+label+`"`) {
			t.Errorf("no cell carries the %q label the narrow layout draws:\n%s", label, body)
		}
	}
	// the mark is not a labelled field: it has no label to draw
	if strings.Contains(body, `data-label=""`) {
		t.Error("the refused mark carries an empty label")
	}
	// checked as a whole selector, not a substring: ".log td.mark:empty"
	// contains ".log td.mark" and would pass for the rule this is about
	if !strings.Contains(body, ".log td.mark { display:block }") {
		t.Error("the narrow layout doesn't take the mark out of the label grid")
	}
	if !strings.Contains(body, ".log td.mark:empty") {
		t.Error("an empty mark still takes room at narrow widths")
	}
}

// TestTheRotationSizeIsConfigurable: the size the file rotates at is a
// setting, not a constant — a portal on a small volume has to be able to
// say a smaller one, and -1 turns it off.
func TestTheRotationSizeIsConfigurable(t *testing.T) {
	dir := t.TempDir()
	// never rotate, whatever is written
	s, err := NewStore(dir, -1)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuditMax() != 0 {
		t.Errorf("a store told never to rotate answers %d", s.AuditMax())
	}
	for i := range 2000 {
		if err := s.Audit(Entry{Team: "team-a", Action: actOff, Account: "claude/" + itoa(i), Result: ResultOK}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, auditOld)); err == nil {
		t.Error("a store told never to rotate rotated")
	}
	// a size is that size
	s2, err := NewStore(t.TempDir(), 500)
	if err != nil {
		t.Fatal(err)
	}
	if s2.AuditMax() != 500 {
		t.Errorf("a store told 500 answers %d", s2.AuditMax())
	}
	// and 0 is the default, which is a real bound
	s3, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if s3.AuditMax() != DefaultAuditMax {
		t.Errorf("a store told nothing answers %d, want the default", s3.AuditMax())
	}
}

// storeAt is a store over dir that rotates audit.jsonl at max bytes.
func storeAt(t *testing.T, dir string, max int64) *Store {
	t.Helper()
	s, err := NewStore(dir, max)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
