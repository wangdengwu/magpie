package teamd

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyText is the team's gateway key as a test mounts it: one key, one line.
const keyText = "sk-magpie-key-teamakeyteamakeyteamakeyteamakeyteamakeyteamakey"

// mountKey puts a team's key where the connect page reads it, and gives
// magpie the same key, so GET /v1/models answers it. Each team has its own
// file: one for the portal would print every team's key on every page.
func mountKey(t *testing.T, f *fixture, secret string) {
	t.Helper()
	writeKey(t, f, "team-a", secret)
	f.magpie.setKey(t, keyA, secret, nil)
}

// writeKey writes team's key file, as ops would mount it.
func writeKey(t *testing.T, f *fixture, teamID, text string) {
	t.Helper()
	dir := filepath.Join(f.srv.cfg.StateDir, KeyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, teamID+".key"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// withGateway tells the fixture where members reach magpie.
func withGateway(t *testing.T, f *fixture, url string) {
	t.Helper()
	f.srv.cfg.GatewayURL = url
}

// TestTheConnectPageSaysWhatAMemberNeeds is the page's whole job: the
// address, the key, both settings forms, and a check that proves it works.
func TestTheConnectPageSaysWhatAMemberNeeds(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")

	rec := get(t, f.srv, "/team/"+f.tokenA+"/connect")
	if rec.Code != http.StatusOK {
		t.Fatalf("the connect page answered %d: %s", rec.Code, rec.Body.String())
	}
	body := pageText(rec.Body.String())
	for _, want := range []string{
		// the address and the key, both as printed settings
		"ANTHROPIC_BASE_URL=https://gateway.example.com",
		"ANTHROPIC_AUTH_TOKEN=" + keyText,
		// the same in Claude Code's own settings file
		`"ANTHROPIC_BASE_URL": "https://gateway.example.com"`,
		`"ANTHROPIC_AUTH_TOKEN": "` + keyText + `"`,
		"~/.claude/settings.json",
		// the check, and what a working answer looks like
		"curl -sS https://gateway.example.com/v1/models",
		"Authorization: Bearer " + keyText,
		"claude/claude-sonnet-5-5",
		// the failures a member meets
		"401", "403", "429",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the connect page doesn't say %q:\n%s", want, body)
		}
	}
	// and the whole key is there, not a shortened form: a member copies it
	if strings.Contains(body, keyText[:20]+"…") || strings.Contains(body, "sk-…") {
		t.Errorf("the key is truncated on the page:\n%s", body)
	}
}

// TestTheSnippetsAreExact: what a member copies is the address and the key
// with nothing around them — the failure a stray space or newline causes is
// a 401 nobody can see.
func TestTheSnippetsAreExact(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com/") // a trailing slash is dropped
	c := Connect{Gateway: "https://gateway.example.com", Key: keyText, KeyState: keyPresent,
		Models: []string{"claude/claude-sonnet-5-5"}}

	wantEnv := "export ANTHROPIC_BASE_URL=https://gateway.example.com\nexport ANTHROPIC_AUTH_TOKEN=" + keyText + "\n"
	if got := snippetEnv(c); got != wantEnv {
		t.Errorf("the env snippet is %q, want %q", got, wantEnv)
	}
	wantJSON := "{\n  \"env\": {\n    \"ANTHROPIC_BASE_URL\": \"https://gateway.example.com\",\n    \"ANTHROPIC_AUTH_TOKEN\": \"" + keyText + "\"\n  }\n}\n"
	if got := snippetJSON(c); got != wantJSON {
		t.Errorf("the settings snippet is %q, want %q", got, wantJSON)
	}
	if got := snippetCheck(c); !strings.Contains(got, "-H \"Authorization: Bearer "+keyText+"\"") {
		t.Errorf("the check doesn't carry the key exactly: %q", got)
	}
	// every line of every snippet ends with a newline and holds no leading
	// or trailing space around a value
	for _, s := range []string{snippetEnv(c), snippetJSON(c)} {
		if strings.Contains(s, " =") || strings.Contains(s, "= ") || strings.Contains(s, " \" ") {
			t.Errorf("a snippet has space around a value: %q", s)
		}
	}
	// the address is the configured one with no trailing slash, whatever was
	// configured
	withGateway(t, f, "https://gateway.example.com/")
	if got := f.srv.gatewayURL(); got != "https://gateway.example.com" {
		t.Errorf("gatewayURL is %q, want the address without its slash", got)
	}
}

// TestTheConnectPageWithoutTheKey: ops may keep the key out of teamd. The
// page still carries the address, the settings and the check, and says the
// member has to paste the key.
func TestTheConnectPageWithoutTheKey(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	for _, want := range []string{
		"ANTHROPIC_BASE_URL=https://gateway.example.com",
		"ANTHROPIC_AUTH_TOKEN=TEAM_KEY",
		"Ops keeps the team key out of this portal",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page without the key doesn't say %q:\n%s", want, body)
		}
	}
	// and it never prints an empty value where the key would be
	if strings.Contains(body, "ANTHROPIC_AUTH_TOKEN=\n") || strings.Contains(body, `"ANTHROPIC_AUTH_TOKEN": ""`) {
		t.Errorf("the page prints an empty key:\n%s", body)
	}
}

// TestAKeyFileThatDoesntHoldOneKeyIsRefused: a file with two keys, or none,
// is not one key to print — the page says so rather than inviting a member
// to copy half of one.
func TestAKeyFileThatDoesntHoldOneKeyIsRefused(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	for name, text := range map[string]string{
		"empty":                "\n\n",
		"two keys":             "sk-magpie-key-one\nsk-magpie-key-two\n",
		"one line with spaces": "sk-magpie-key one\n",
	} {
		writeKey(t, f, "team-a", text)
		body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
		if !strings.Contains(body, "doesn't hold one key") {
			t.Errorf("a key file with %q isn't refused:\n%s", name, body)
		}
		if strings.Contains(body, "ANTHROPIC_AUTH_TOKEN=sk-magpie-key-one") {
			t.Errorf("a key file with %q put a key on the page:\n%s", name, body)
		}
	}
}

// TestTheMountingKeyIsCheckedAgainstTheGateway: a key file that doesn't
// hold the team's current key would print snippets that fail with 401. The
// page asks the gateway with that key, and says when it is refused.
func TestTheMountingKeyIsCheckedAgainstTheGateway(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	// the file holds a key magpie doesn't know
	writeKey(t, f, "team-a", "sk-magpie-key-rotated-away\n")
	f.magpie.setKey(t, keyA, keyText, nil)
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	if !strings.Contains(body, "The gateway refuses the key mounted for this page") {
		t.Errorf("a stale mounted key isn't said to be refused:\n%s", body)
	}
}

// TestTheModelsAreTheKeysOwn is the filtering: the page lists what this
// key may use, and another team's models are never among them.
func TestTheModelsAreTheKeysOwn(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")
	// the key is held to two of the three models
	f.magpie.setKey(t, keyA, keyText, []string{"claude/claude-sonnet-5-5", "claude/claude-haiku-5-5"})
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	for _, want := range []string{"claude/claude-sonnet-5-5", "claude/claude-haiku-5-5"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't list %q, which the key may use:\n%s", want, body)
		}
	}
	if strings.Contains(body, "claude/claude-opus-5-5") {
		t.Errorf("the page lists a model the key may not use:\n%s", body)
	}
	// an account or a key of the provider is not a model a member names
	for _, notModel := range []string{"alice@example.com", "relay-key"} {
		if strings.Contains(body, notModel) {
			t.Errorf("the page lists %q as a model:\n%s", notModel, body)
		}
	}
}

// TestThePageCarriesNoScriptAndNoLinkToAnotherOrigin: the page has the
// team's key on it. It may not run script, load anything, or be framed, and
// it may not put the key anywhere a request would carry it.
func TestThePageCarriesNoScriptAndNoLinkToAnotherOrigin(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")
	rec := get(t, f.srv, "/team/"+f.tokenA+"/connect")
	body := rec.Body.String()
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy is %q, want no-referrer", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("the page's CSP is %q, which doesn't say %q", csp, want)
		}
	}
	for _, bad := range []string{"<script", "src=", "<link", "<iframe"} {
		if strings.Contains(body, bad) {
			t.Errorf("the page holds %q:\n%s", bad, body)
		}
	}
	// the key is never in a URL: not in an href, a form's action, or a query
	for _, form := range strings.Split(body, "<form")[1:] {
		if strings.Contains(form, keyText) {
			t.Errorf("the key is in a form's action:\n%s", form[:min(len(form), 300)])
		}
	}
	for _, a := range strings.Split(body, "<a ")[1:] {
		if i := strings.Index(a, ">"); i > 0 && strings.Contains(a[:i], keyText) {
			t.Errorf("the key is in a link: %s", a[:i])
		}
	}
}

// TestTheConnectPageIsTheTeamsOwn: another team's token reads its own key
// and its own models, and an unknown token reads nothing.
func TestTheConnectPageIsTheTeamsOwn(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")
	const keyBText = "sk-magpie-key-teambteambteambteambteamb"
	writeKey(t, f, "team-b", keyBText+"\n")
	f.magpie.setKey(t, keyB, keyBText, []string{"claude/claude-opus-5-5"})

	body := pageText(get(t, f.srv, "/team/"+f.tokenB+"/connect").Body.String())
	if !strings.Contains(body, "sk-magpie-key-teambteambteambteambteamb") {
		t.Errorf("team B's page doesn't carry its own key:\n%s", body)
	}
	for _, leak := range []string{keyText, "claude/claude-sonnet-5-5", groupA, "team-a"} {
		if strings.Contains(body, leak) {
			t.Errorf("team B's page names %q, which is team A's:\n%s", leak, body)
		}
	}
	if rec := get(t, f.srv, "/team/nope/connect"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown token answered %d, want 404", rec.Code)
	}
}

// TestThePageSaysWhenTheGatewayAddressIsMissing: a snippet with no address
// is worse than none, so the page says the portal wasn't told one.
func TestThePageSaysWhenTheGatewayAddressIsMissing(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	// no -gateway-url
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	if !strings.Contains(body, "wasn't told the gateway's address") {
		t.Errorf("the page doesn't say the address is missing:\n%s", body)
	}
	if strings.Contains(body, "ANTHROPIC_BASE_URL=\n") {
		t.Errorf("the page prints an empty address:\n%s", body)
	}
}

// TestTheFailureListNamesWhatToCheck: the three answers a member actually
// meets, each with what it means.
func TestTheFailureListNamesWhatToCheck(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	for _, c := range []struct{ code, means string }{
		{"401", "the key isn't one this gateway takes"},
		{"403", "the key isn't allowed here"},
		{"429", "the team's budget is spent"},
	} {
		if !strings.Contains(body, c.code) || !strings.Contains(body, c.means) {
			t.Errorf("the failure list doesn't name %s as %q:\n%s", c.code, c.means, body)
		}
	}
}

// TestTheFormMustComeFromThePortalsOwnPage is not a route of this slice:
// the connect page is a read. What it holds is that the page has no form
// that posts anywhere, so nothing of it can be submitted with the key.
func TestTheConnectPageHasNoForm(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")
	body := get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String()
	if strings.Contains(body, "<form") {
		t.Errorf("the connect page carries a form:\n%s", body)
	}
	_ = url.Values{}
}

// TestModelsWithoutAMountedKeyAreTheKeysOwn: with no key mounted, magpie's
// management API is asked and the list filtered here — and an account's id
// is not a model, though it has the same "<provider>/<id>" shape.
func TestModelsWithoutAMountedKeyAreTheKeysOwn(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	// no key file: the page goes through the management API
	f.magpie.setKey(t, keyA, keyText, []string{"claude/claude-sonnet-5-5"})

	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	if !strings.Contains(body, "claude/claude-sonnet-5-5") {
		t.Errorf("the page doesn't list the model the key may use:\n%s", body)
	}
	for _, notModel := range []string{"claude/claude-opus-5-5", "claude/1a2b3c4d5e6f7a80", "claude/90fedcba87654321", "relay-key"} {
		if strings.Contains(body, notModel) {
			t.Errorf("the page lists %q, which is not a model this key may use:\n%s", notModel, body)
		}
	}
}

// TestModelsForAKeyHeldToNothing is every model, and none of the accounts.
func TestModelsForAKeyHeldToNothing(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	ids, complete, err := f.srv.ModelsFor(t.Context(), keyA, "")
	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Error("a key held to nothing has the whole list")
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "claude/1a2b") || strings.HasPrefix(id, "claude/0f0e") || strings.HasPrefix(id, "claude/90fe") {
			t.Errorf("an account's id is listed as a model: %q of %q", id, ids)
		}
	}
	if len(ids) != 3 {
		t.Errorf("the list is %q, want the three models", ids)
	}
}

// TestAKeyHeldToAGroupSaysTheListMayBeShort: a routing group is the
// gateway's to expand, and this side can't, so it says so rather than
// pretending the list is whole.
func TestAKeyHeldToAGroupSaysTheListMayBeShort(t *testing.T) {
	f := newFixture(t)
	withGateway(t, f, "https://gateway.example.com")
	f.magpie.setKey(t, keyA, keyText, []string{"group/fast", "claude/claude-sonnet-5-5"})
	_, complete, err := f.srv.ModelsFor(t.Context(), keyA, "")
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Error("a key held to a routing group is reported as a whole list")
	}
	body := pageText(get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String())
	if !strings.Contains(body, "any others a routing group this key names adds") {
		t.Errorf("the page doesn't say the list may be short:\n%s", body)
	}
}

// TestNoPagePrintsAnEmptyValue: with no address and no key, the page says
// so and prints no empty setting. An exported empty ANTHROPIC_BASE_URL
// would send a member's requests to Anthropic's own endpoint with a magpie
// key, which is worse than a page that says what's missing.
func TestNoPagePrintsAnEmptyValue(t *testing.T) {
	f := newFixture(t)
	// neither -gateway-url nor a key file
	rec := get(t, f.srv, "/team/"+f.tokenA+"/connect")
	body := pageText(rec.Body.String())
	for _, bad := range []string{
		"ANTHROPIC_BASE_URL=\n", "ANTHROPIC_BASE_URL=\t", `"ANTHROPIC_BASE_URL": ""`,
		"ANTHROPIC_AUTH_TOKEN=\n", `"ANTHROPIC_AUTH_TOKEN": ""`,
		"<span class=\"mono sel\"></span>", "the pool is",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("the page prints %q:\n%s", bad, body)
		}
	}
	// and it still says what is wrong and what to do
	for _, want := range []string{"wasn't told the gateway's address", "Ops keeps the team key out of this portal"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't say %q:\n%s", want, body)
		}
	}
}

// TestTheSnippetsNeedAnAddress is the guard behind the page's own: a
// snippet function handed no address answers nothing, whatever calls it.
// The page avoids drawing one at all, and this is the second half — a
// caller that prints its result can't export an empty ANTHROPIC_BASE_URL,
// which would send the member's requests to Anthropic's own endpoint.
func TestTheSnippetsNeedAnAddress(t *testing.T) {
	c := Connect{Key: keyText, KeyState: keyPresent, Models: []string{"claude/claude-sonnet-5-5"}}
	for name, got := range map[string]string{
		"env":   snippetEnv(c),
		"json":  snippetJSON(c),
		"check": snippetCheck(c),
	} {
		if got != "" {
			t.Errorf("the %s snippet with no address is %q, want nothing", name, got)
		}
	}
}

// TestTheCopiedBlocksWrapNothing: a key that wraps inside a settings file
// is still valid JSON but is not what a reader sees themselves copy — the
// block must scroll instead, so the key stays one run of characters on one
// line.
func TestTheCopiedBlocksWrapNothing(t *testing.T) {
	f := newFixture(t)
	mountKey(t, f, keyText)
	withGateway(t, f, "https://gateway.example.com")
	body := get(t, f.srv, "/team/"+f.tokenA+"/connect").Body.String()
	// the .code rule, which is what a copied block is drawn with. (.link,
	// on the sign-in page, does wrap — a vendor's URL is not copied whole
	// from here but opened, so it may break anywhere.)
	i := strings.Index(body, ".code pre {")
	if i < 0 {
		t.Fatalf("the page has no .code pre rule:\n%s", body[:min(len(body), 4000)])
	}
	rule := body[i:]
	if j := strings.Index(rule, "}"); j > 0 {
		rule = rule[:j]
	}
	if strings.Contains(rule, "pre-wrap") || strings.Contains(rule, "overflow-wrap") {
		t.Errorf("a code block wraps its content: %s", rule)
	}
	if !strings.Contains(rule, "white-space:pre;") {
		t.Errorf("a code block doesn't keep its lines whole: %s", rule)
	}
}
