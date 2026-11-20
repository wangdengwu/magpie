package teamd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestLangOfReadsAcceptLanguage is the language picker: the header is a list
// of tags with weights, and the highest-weighted one this build speaks wins.
func TestLangOfReadsAcceptLanguage(t *testing.T) {
	for _, c := range []struct {
		header string
		want   lang
	}{
		{"", langEN},
		{"zh-CN", langZH},
		{"zh", langZH},
		{"zh-Hans", langZH},
		{"zh-Hans-CN", langZH},
		{"zh-TW", langZH}, // Traditional is not a third language here: see below
		{"en-US,en;q=0.9", langEN},
		{"en-GB", langEN},
		// the weights decide, not the order
		{"zh-CN,zh;q=0.9,en;q=0.8", langZH},
		{"en;q=0.8,zh;q=0.9", langZH},
		{"en,zh;q=0.9", langEN},
		// a specific tag beats a general one at the same weight
		{"zh-CN;q=0.9,en-US;q=0.9", langZH},
		// nothing this build speaks
		{"*", langEN},
		{"fr-FR,fr;q=0.9", langEN},
		{"de", langEN},
		// a weight that can't be read drops its own tag, not the header
		{"zh;q=abc,en;q=0.5", langEN},
		{"zh;q=2,en;q=0.5", langEN},
		{" , ,zh", langZH},
		{"ZH-cn", langZH},
	} {
		if got := langOf(c.header); got != c.want {
			t.Errorf("langOf(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

// TestTheWordListsAgree says the two lists hold the same keys and no blank
// sentence: a language that leaves a sentence out reads English's, but one
// that writes an empty string would draw nothing.
func TestTheWordListsAgree(t *testing.T) {
	en, zh := sortedKeys(enWords), sortedKeys(zhWords)
	if strings.Join(en, ",") != strings.Join(zh, ",") {
		onlyEN, onlyZH := difference(en, zh), difference(zh, en)
		t.Fatalf("the lists' keys differ:\n only en: %v\n only zh: %v", onlyEN, onlyZH)
	}
	if len(en) < 150 {
		t.Fatalf("the word list holds %d keys, which is too few to be all of it", len(en))
	}
	for _, l := range langs() {
		for k, v := range wordList(l) {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s says nothing for %q", l, k)
			}
		}
	}
}

// TestEveryWordIsUsed says the list carries no sentence nothing says: a key
// no template and no Go file names is a sentence nobody translated for a
// reason.
func TestEveryWordIsUsed(t *testing.T) {
	used := map[string]bool{}
	for _, k := range wordKeysIn(packageFiles(t)) {
		used[k] = true
	}
	src := packageFiles(t)
	for k := range enWords {
		// a `.one` entry rides on its own key's call, which is Tn's
		base := strings.TrimSuffix(k, ".one")
		if used[base] || used[k] || strings.Contains(src, `"`+k+`"`) {
			continue
		}
		t.Errorf("%q is in the word list and nothing says it", k)
	}
}

// TestEverySentenceIsTranslated: each page says its sentences in Chinese,
// and none of them is the English one. A sentence left in English is a
// missing entry, whether the list has it or not.
func TestEverySentenceIsTranslated(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	memberSpends(f)
	paths := []string{
		"/team/" + f.tokenA,
		"/team/" + f.tokenA + "/usage",
		"/team/" + f.tokenA + "/connect",
		"/team/" + f.tokenA + "/members",
		"/team/" + f.tokenA + "/audit",
		"/team/" + f.tokenA + "/add",
	}
	for _, path := range paths {
		zh := pageText(accepting(t, f, path, "zh-CN").Body.String())
		if !strings.Contains(zh, `lang="zh"`) {
			t.Errorf("%s: the page doesn't say its own language:\n%s", path, zh)
		}
		if got := accepting(t, f, path, "zh-CN").Header().Get("Content-Language"); got != "zh" {
			t.Errorf("%s: Content-Language is %q, want zh", path, got)
		}
		if strings.Contains(zh, `T "`) || strings.Contains(zh, "{{.") {
			t.Errorf("%s: the page shows a word key or a placeholder:\n%s", path, zh)
		}
	}
}

// TestNoSentenceIsTheSameInBoth: a word list's entry that says what English
// says is an entry nobody translated. A sentence that is only names, ids and
// punctuation is the same on purpose, and is left out.
func TestNoSentenceIsTheSameInBoth(t *testing.T) {
	for k, en := range enWords {
		zh, ok := zhWords[k]
		if !ok {
			t.Errorf("%q is in English's list and not Chinese's", k)
			continue
		}
		if en == zh && !namesOnly(en) {
			t.Errorf("%q is the English sentence in both: %q", k, en)
		}
	}
}

// namesOnly says a sentence is only values and punctuation, which no
// language would say differently: "teamd", "{{.N}}m", "{{.H}}h {{.M}}m".
func namesOnly(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			continue
		}
	}
	// a sentence with no letters of its own is a format, not words
	words := strings.FieldsFunc(s, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') })
	for _, w := range words {
		switch strings.ToLower(w) {
		case "teamd", "api", "tokens", "claude":
			// a product's name, and a unit magpie's own pages use too
			continue
		}
		return false
	}
	return true
}

// TestEnglishPagesAreUnchanged: an English request gets what it got before
// this change, sentence for sentence.
func TestEnglishPagesAreUnchanged(t *testing.T) {
	f := newFixture(t)
	issueMembers(t, f)
	memberSpends(f)
	body := pageText(accepting(t, f, "/team/"+f.tokenA, "en-US").Body.String())
	for _, want := range []string{
		"Account pool",
		"2 of 2 accounts in use",
		"account group",
		"Usage", "Connect", "Member keys", "History", "Add an account",
		"in use", "Max", "Pro",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the English page doesn't say %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `lang="en"`) {
		t.Errorf("the English page doesn't say its own language:\n%s", body)
	}
	if got := accepting(t, f, "/team/"+f.tokenA, "en-US").Header().Get("Content-Language"); got != "en" {
		t.Errorf("Content-Language is %q, want en", got)
	}
}

// TestAMissingWordFallsBackToEnglish: deleting a Chinese sentence draws the
// English one, never a blank and never the key.
func TestAMissingWordFallsBackToEnglish(t *testing.T) {
	saved := zhWords["accounts.empty"]
	delete(zhWords, "accounts.empty")
	t.Cleanup(func() { zhWords["accounts.empty"] = saved })

	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "Team A"},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	body := pageText(accepting(t, f, "/team/"+f.tokenA, "zh-CN").Body.String())
	if !strings.Contains(body, "This team's pool is empty") {
		t.Errorf("a missing Chinese sentence didn't fall back to English:\n%s", body)
	}
	if strings.Contains(body, "accounts.empty") {
		t.Errorf("the page shows a word key:\n%s", body)
	}
}

// TestANameIsEscapedInBothLanguages: a team or an account name holding
// markup is text in either language, since it is an argument to a sentence.
func TestANameIsEscapedInBothLanguages(t *testing.T) {
	f := newFixture(t)
	f.magpie.set([]Group{
		{ID: groupA, Name: "<script>alert(1)</script>", Accounts: []string{acctA1}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
	})
	for _, lang := range []string{"en-US", "zh-CN"} {
		body := accepting(t, f, "/team/"+f.tokenA, lang).Body.String()
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Errorf("%s: a team name put markup in the page:\n%s", lang, body)
		}
		if !strings.Contains(body, "&lt;script&gt;") {
			t.Errorf("%s: the team name isn't shown as text:\n%s", lang, body)
		}
	}
}

// TestThePortalSpeaksOneLanguagePerRequest: two requests to one page, with
// different headers, are answered in each — there is no cookie and no
// stored preference, so one browser's language never reaches another's.
func TestThePortalSpeaksOneLanguagePerRequest(t *testing.T) {
	f := newFixture(t)
	first := pageText(accepting(t, f, "/team/"+f.tokenA, "zh-CN").Body.String())
	second := pageText(accepting(t, f, "/team/"+f.tokenA, "en-US").Body.String())
	if !strings.Contains(first, "账号池") {
		t.Errorf("the first request wasn't Chinese:\n%s", first)
	}
	if !strings.Contains(second, "Account pool") || strings.Contains(second, "账号池") {
		t.Errorf("the second request wasn't English:\n%s", second)
	}
}

// accepting asks for a page with a browser's Accept-Language.
func accepting(t *testing.T, f *fixture, path, lang string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Accept-Language", lang)
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s (%s) answered %d: %s", path, lang, rec.Code, rec.Body.String())
	}
	return rec
}

// sortedKeys and langs are the word lists' own, for a guard that compares
// them.
func sortedKeys(w words) []string {
	out := make([]string, 0, len(w))
	for k := range w {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func langs() []lang { return languages }

// difference is the keys of a that b doesn't have.
func difference(a, b []string) []string {
	has := map[string]bool{}
	for _, k := range b {
		has[k] = true
	}
	var out []string
	for _, k := range a {
		if !has[k] {
			out = append(out, k)
		}
	}
	return out
}

// packageFiles is this package's own source, the templates and the Go that
// says a sentence through t.T both: a word list's key is used if any of it
// names the key.
func packageFiles(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(src)
	}
	if b.Len() == 0 {
		t.Fatal("read no source")
	}
	return b.String()
}

// wordKeysIn is the word keys a source names: a sentence it says, and one it
// keeps in a table for a page to say later, which is a plain string there.
func wordKeysIn(src string) []string {
	var out []string
	for _, quote := range []string{`T("`, `Tn("`, `T "`, `Tn "`} {
		for i := 0; ; {
			j := strings.Index(src[i:], quote)
			if j < 0 {
				break
			}
			rest := src[i+j+len(quote):]
			k, _, ok := strings.Cut(rest, `"`)
			if !ok {
				break
			}
			out = append(out, k)
			i += j + len(quote) + len(k) + 1
		}
	}
	return out
}

// TestALinkThatOpensNothingIsSaidInBothLanguages: a 404 says the same thing
// in either language, and says nothing about why — the boundary's answer for
// a token that names no team is the same for a wrong one and a guessed one.
func TestALinkThatOpensNothingIsSaidInBothLanguages(t *testing.T) {
	f := newFixture(t)
	for lang, want := range map[string]string{"zh-CN": "这个链接打不开", "en-US": "This link opens nothing"} {
		r := httptest.NewRequest(http.MethodGet, "/team/nope", nil)
		r.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: a link that names no team answered %d", lang, rec.Code)
		}
		if body := pageText(rec.Body.String()); !strings.Contains(body, want) {
			t.Errorf("%s: the 404 doesn't say %q:\n%s", lang, want, body)
		}
	}
}

// TestAnUnknownAuditActionIsShownAsItIs: a line written by a newer teamd
// carries an action this build has no words for, and it reads as that
// action in either language — never as a key, and never dropped.
func TestAnUnknownAuditActionIsShownAsItIs(t *testing.T) {
	f := newFixture(t)
	if err := f.srv.Store().Audit(Entry{
		At: time.Now().UTC(), Team: "team-a", Action: "from-a-newer-teamd",
		Who: "team-a", Result: ResultOK,
	}); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en-US", "zh-CN"} {
		body := pageText(accepting(t, f, "/team/"+f.tokenA+"/audit", lang).Body.String())
		if !strings.Contains(body, "from-a-newer-teamd") {
			t.Errorf("%s: an unknown action isn't shown:\n%s", lang, body)
		}
	}
}

// TestAnAuditLineIsTranslated: the words the page says an action in are the
// request's language, not a verb stored with the line.
func TestAnAuditLineIsTranslated(t *testing.T) {
	f := newFixture(t)
	if err := f.srv.Store().Audit(Entry{
		At: time.Now().UTC(), Team: "team-a", Action: actOff, Account: acctA2,
		Who: "team-a", Result: ResultOK,
	}); err != nil {
		t.Fatal(err)
	}
	zh := pageText(accepting(t, f, "/team/"+f.tokenA+"/audit", "zh-CN").Body.String())
	if !strings.Contains(zh, "停用了账号") {
		t.Errorf("the Chinese history doesn't say what happened:\n%s", zh)
	}
	if strings.Contains(zh, "disabled an account") {
		t.Errorf("the Chinese history carries the English verb:\n%s", zh)
	}
}

// TestTheConnectPagesCodeBlocksAreTheSameInBoth: the snippets a member
// copies are shell and JSON, and no language changes them; only the prose
// around them is translated.
func TestTheConnectPagesCodeBlocksAreTheSameInBoth(t *testing.T) {
	f := newFixture(t)
	// a portal that knows the gateway's address prints the snippets
	f.srv.cfg.GatewayURL = "https://gateway.example.com"
	blocks := func(body string) []string {
		var out []string
		for i := 0; ; {
			j := strings.Index(body[i:], `<pre class="sel">`)
			if j < 0 {
				return out
			}
			rest := body[i+j+len(`<pre class="sel">`):]
			k := strings.Index(rest, `</pre>`)
			if k < 0 {
				return out
			}
			out = append(out, rest[:k])
			i += j + len(`<pre class="sel">`) + k + len(`</pre>`)
		}
	}
	en := blocks(accepting(t, f, "/team/"+f.tokenA+"/connect", "en-US").Body.String())
	zh := blocks(accepting(t, f, "/team/"+f.tokenA+"/connect", "zh-CN").Body.String())
	if len(en) < 3 || len(en) != len(zh) {
		t.Fatalf("the two pages hold %d and %d code blocks", len(en), len(zh))
	}
	for i := range en {
		if en[i] != zh[i] {
			t.Errorf("code block %d differs:\n en: %q\n zh: %q", i, en[i], zh[i])
		}
	}
}
