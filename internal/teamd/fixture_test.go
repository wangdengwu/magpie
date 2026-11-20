package teamd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// testAdminKey is the "admin local" gateway key teamd holds. Never a real
// one: a test's key is written to a file in its own folder.
const testAdminKey = "sk-magpie-key-0000000000000000000000000000000000000000000000000000000000000000"

// The two teams every boundary test uses, and the accounts their groups
// hold. Group ids are the short hex magpie makes; accounts are
// "<provider>/<stable id>", as a group keeps them.
const (
	groupA = "6f1c2a9d4b70"
	groupB = "b3d8e04c17a2"
	groupC = "c1a2b3c4d5e6"
	acctA1 = "claude/1a2b3c4d5e6f7a80"
	acctA2 = "claude/90fedcba87654321"
	acctB1 = "claude/0f0e0d0c0b0a0908"
	// acctOps is an account ops adds to a group, for the tests about a
	// change made while a sign-in is open.
	acctOps = "claude/aaaabbbbccccdddd"
	keyA    = "aaaa1111bbbb2222cccc3333"
	keyB    = "dddd4444eeee5555ffff6666"
	keyC    = "abcdabcdabcdabcdabcdabcd"
	keyOff  = "999988887777666655554444"
)

// fakeMagpie is magpie's management API, as far as teamd calls it. It
// records what it was sent, so a test can say what reached magpie and what
// never did.
type fakeMagpie struct {
	*httptest.Server
	mu     sync.Mutex
	groups []Group
	keys   []GatewayKey
	// status and body answer instead of groups, for a magpie that fails.
	status int
	body   string
	// echoKey puts the caller's Authorization header in a 500's body, to
	// prove a reply that repeats the key can't carry it into an error.
	echoKey bool
	// models is GET /gateway-keys/models's accounts: every account and key
	// magpie has, with the id a group keeps it by.
	models []map[string]any
	// providers is GET /providers, raw, as magpie sends it.
	providers []map[string]any
	// usage is GET /login/usage?agent=claude.
	usage map[string]any
	// calls are the requests made, by path.
	calls []string
	// signIns are the sign-ins magpie has, by id, as its routes answer
	// them; signed is the account each one made.
	signIns map[string]map[string]any
	signed  map[string]string
	// pastes are the addresses handed to signin/{id}/callback, in order.
	pastes []string
	// secrets are the gateway key secrets GET /v1/models takes, by key id.
	secrets map[string]string
	// refuseModels, when set, is the status GET /v1/models answers with.
	refuseModels int
	// canceled are the sign-in ids cancelled.
	canceled []string
	// onSignIn, when set, runs on a POST signin before magpie answers, so a
	// test can make the account appear as the vendor's would.
	onSignIn func(f *fakeMagpie)
	// onCallback, when set, runs while a paste is being handled — the call
	// that blocks until the vendor's sign-in is over, which is when ops can
	// change a group unbeknownst to the request that is waiting.
	onCallback func(f *fakeMagpie)
	// logins are the POST login/{on,off} calls that reached magpie, in
	// order, as "on claude <user>".
	logins []string
	// onLogin, when set, answers instead of magpie's own change: a
	// non-empty string is magpie's refusal, "" means it took the call and
	// changed nothing — a vendor-rested account, which is the case a page
	// that believed its own request would draw wrong.
	onLogin func(f *fakeMagpie, on bool, user string) string
	// issued are the member keys the portal issued, and the fields it sent
	// with each: the issuer, the name and the lifetime.
	issued   []MemberKey
	issueAsk []issueAsk
	// expiresForNextIssue makes the next issued key end then, whatever
	// lifetime was asked for, so a test can place a key in the far future.
	expiresForNextIssue time.Time
	// revoked are the revokes that reached magpie, as "<issuer>/<id>".
	revoked []string
	// refuseRevoke makes a revoke answer 404, as another issuer's does.
	refuseRevoke bool
	// spends are what each member key used, by "<issuer>/<period>", as
	// gateway-keys/issuer-usage answers them.
	spends map[string]IssuerUsage
	// refuseSpend, when set, is the status issuer-usage answers with, for
	// the periods it names: a read that fails is what a page must say
	// rather than draw as zeros.
	refuseSpend map[string]int
}

// setSpend makes one key's spend over one period, as magpie's ledger has
// it. The issuer is the team's gateway key; a key not in the team's live
// list is one revoked since, and still counts.
func (f *fakeMagpie) setSpend(issuer, period string, rows ...IssuerSpend) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spends == nil {
		f.spends = map[string]IssuerUsage{}
	}
	k := issuer + "/" + period
	u := f.spends[k]
	u.Period, u.Keys = period, append(u.Keys, rows...)
	for _, r := range rows {
		u.Total.Calls += r.Calls
		u.Total.Input += r.Input
		u.Total.Output += r.Output
		u.Total.Cost += r.Cost
	}
	f.spends[k] = u
}

// issueAsk is what an issue-key call sent.
type issueAsk struct {
	Issuer string
	Name   string
	TTL    time.Duration
}

// issuedUnder is the issuer of every issue-key call that reached magpie.
func (f *fakeMagpie) issuedUnder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.issueAsk))
	for _, a := range f.issueAsk {
		out = append(out, a.Issuer)
	}
	return out
}

func (f *fakeMagpie) lastIssuedTTL() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.issueAsk) == 0 {
		return 0
	}
	return f.issueAsk[len(f.issueAsk)-1].TTL
}

func (f *fakeMagpie) lastIssuedKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.issued) == 0 {
		return ""
	}
	return f.issued[len(f.issued)-1].Key
}

func (f *fakeMagpie) revokedUnder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}

func newFakeMagpie(t *testing.T, groups []Group) *fakeMagpie {
	t.Helper()
	f := &fakeMagpie{
		groups: groups,
		keys: []GatewayKey{
			// team A's key has two caps, which is the case the usage page
			// has to draw: a request passes only while both have room.
			// Each key names its team's group, as a real one must: a key
			// that names no account reaches every account magpie has, and
			// Ops.Add refuses it (holdsGroup).
			{ID: keyA, Name: "team-a", Issues: true, Accounts: []string{"@" + groupA}, Limit: &Limit{Period: "day", Tokens: 2_000_000}, MoreLimits: []Limit{{Period: "hours", Hours: 5, Cost: 5}},
				UsedAll: []LimitUse{
					{Period: "day", Calls: 41, Tokens: 1_250_000, TokenLimit: 2_000_000, TokensLeft: 750_000,
						Start: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), Reset: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)},
					{Period: "hours", Hours: 5, Calls: 41, Cost: 1.25, CostLimit: 5, CostLeft: 3.75,
						Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Reset: time.Date(2026, 11, 18, 14, 0, 0, 0, time.UTC)},
				}},
			{ID: keyB, Name: "team-b", Accounts: []string{"@" + groupB}},
			{ID: keyC, Name: "team-c", Accounts: []string{"@" + groupC}},
			{ID: keyOff, Name: "old", Off: true, Accounts: []string{"@" + groupC}},
		},
		models:    claudeModels(),
		usage:     claudeUsage(),
		providers: claudeProviders(),
		signIns:   map[string]map[string]any{},
		signed:    map[string]string{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMagpie) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	groups, keys, status, body, echo := f.groups, f.keys, f.status, f.body, f.echoKey
	models, provs, usage := f.models, f.providers, f.usage
	f.mu.Unlock()

	if p := strings.TrimPrefix(r.URL.Path, AdminPath); p != r.URL.Path && strings.HasPrefix(p, "signin") {
		f.serveSignIn(w, r)
		return
	}
	if p := strings.TrimPrefix(r.URL.Path, AdminPath); p == "login/on" || p == "login/off" {
		f.serveLogin(w, r, p == "login/on")
		return
	}
	// the gateway's own model list, which the connect page asks for with the
	// team's key when it is mounted: it needs that key, not the admin one
	if r.URL.Path == "/v1/models" {
		f.serveModels(w, r)
		return
	}
	switch r.URL.Path {
	case AdminPath + "account-groups", AdminPath + "gateway-keys", AdminPath + "gateway-keys/models",
		AdminPath + "providers", AdminPath + "login/usage", AdminPath + "account-groups/save",
		AdminPath + "gateway-keys/issue-key", AdminPath + "gateway-keys/issued-keys", AdminPath + "gateway-keys/revoke-key",
		AdminPath + "gateway-keys/issuer-usage":
	default:
		http.NotFound(w, r)
		return
	}
	if got, want := r.Header.Get("Authorization"), "Bearer "+testAdminKey; got != want {
		// a wrong key is magpie's 401, and the test's own failure: nothing
		// else in teamd should ever have called this without the key
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"the management API needs a gateway key that may manage magpie"}`)
		return
	}
	if echo {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "the management API refused %s", r.Header.Get("Authorization"))
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		io.WriteString(w, body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case AdminPath + "gateway-keys/issue-key":
		var in struct {
			Issuer     string `json:"issuer"`
			Name       string `json:"name"`
			TTLSeconds int64  `json:"ttlSeconds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		k := MemberKey{ID: "member" + strconv.Itoa(len(f.issued)+1), Name: in.Name, Issuer: in.Issuer,
			Key: "sk-magpie-key-issued" + strconv.Itoa(len(f.issued)+1)}
		k.Expires = time.Now().Add(time.Duration(in.TTLSeconds) * time.Second)
		f.mu.Lock()
		if !f.expiresForNextIssue.IsZero() {
			k.Expires = f.expiresForNextIssue
		}
		f.issueAsk = append(f.issueAsk, issueAsk{Issuer: in.Issuer, Name: in.Name, TTL: time.Duration(in.TTLSeconds) * time.Second})
		f.issued = append(f.issued, k)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(k)
	case AdminPath + "gateway-keys/issued-keys":
		f.mu.Lock()
		keys := make([]MemberKey, 0, len(f.issued))
		for _, k := range f.issued {
			k.Key = "" // magpie never lists a secret
			keys = append(keys, k)
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	case AdminPath + "gateway-keys/revoke-key":
		var in struct {
			Issuer string `json:"issuer"`
			ID     string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		refuse := f.refuseRevoke
		for _, k := range f.issued {
			if k.ID == in.ID && k.Issuer != in.Issuer {
				refuse = true // another issuer's key is "not found"
			}
		}
		if !refuse {
			f.revoked = append(f.revoked, in.Issuer+"/"+in.ID)
			kept := f.issued[:0]
			for _, k := range f.issued {
				if k.ID != in.ID {
					kept = append(kept, k)
				}
			}
			f.issued = kept
		}
		f.mu.Unlock()
		if refuse {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"no key issued under that issuer has the id"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revoked": in.ID})
	case AdminPath + "account-groups/save":
		var g Group
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, err.Error())
			return
		}
		f.mu.Lock()
		found := false
		for i := range f.groups {
			if strings.EqualFold(f.groups[i].ID, g.ID) {
				f.groups[i] = g
				found = true
			}
		}
		groups := f.groups
		f.mu.Unlock()
		if !found {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"no such group %s"}`, g.ID)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups})
	case AdminPath + "gateway-keys/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": models})
	case AdminPath + "gateway-keys":
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	case AdminPath + "providers":
		_ = json.NewEncoder(w).Encode(map[string]any{"providers": provs})
	case AdminPath + "login/usage":
		_ = json.NewEncoder(w).Encode(usage)
	case AdminPath + "gateway-keys/issuer-usage":
		issuer, period := r.URL.Query().Get("issuer"), r.URL.Query().Get("period")
		f.mu.Lock()
		u, refuse := f.spends[issuer+"/"+period], f.refuseSpend[period]
		f.mu.Unlock()
		if refuse != 0 {
			w.WriteHeader(refuse)
			fmt.Fprintf(w, `{"error":"the ledger can't be read"}`)
			return
		}
		u.Period = period
		if u.Keys == nil {
			u.Keys = []IssuerSpend{}
		}
		_ = json.NewEncoder(w).Encode(u)
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups})
	}
}

// callsTo is how many requests reached f.
func (f *fakeMagpie) callsTo() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// set replaces the groups magpie reports.
func (f *fakeMagpie) set(groups []Group) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups = groups
}

// fail makes every answer an error.
func (f *fakeMagpie) fail(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

// fixture is a running teamd over a fake magpie, with two teams that each
// hold a token and an account group.
type fixture struct {
	srv    *Server
	magpie *fakeMagpie
	teams  []Team
	tokenA string
	tokenB string
	dir    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	magpie := newFakeMagpie(t, []Group{
		{ID: groupA, Name: "Team A", Accounts: []string{acctA1, acctA2}},
		{ID: groupB, Name: "Team B", Accounts: []string{acctB1}},
		{ID: groupC, Name: "Team C"},
	})
	tokenA, hashA, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tokenB, hashB, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := Config{
		AdminURL:     magpie.URL,
		AdminKeyFile: filepath.Join(dir, "admin.key"),
		Listen:       "127.0.0.1:0",
		StateDir:     filepath.Join(dir, "state"),
	}
	if err := os.WriteFile(cfg.AdminKeyFile, []byte(testAdminKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		srv:    srv,
		magpie: magpie,
		dir:    dir,
		tokenA: tokenA,
		tokenB: tokenB,
		teams: []Team{
			{ID: "team-a", Name: "Team A", GroupID: groupA, KeyID: keyA, TokenHash: hashA},
			{ID: "team-b", Name: "Team B", GroupID: groupB, KeyID: keyB, TokenHash: hashB},
		},
	}
	if err := srv.Store().Save(f.teams); err != nil {
		t.Fatal(err)
	}
	return f
}

// claudeModels is GET /gateway-keys/models's accounts: two of team A's,
// one of team B's, and an API key of the provider's.
func claudeModels() []map[string]any {
	acct := func(id, name, plan string) map[string]any {
		return map[string]any{"id": id, "name": name, "provider": "claude", "providerName": "Claude", "account": true, "plan": plan}
	}
	return []map[string]any{
		acct(acctA1, "alice@example.com", "Max"),
		acct(acctA2, "bob@example.com", "Pro"),
		acct(acctB1, "carol@example.com", "Pro"),
		{"id": "claude/9999999999999999", "name": "relay-key", "provider": "claude", "providerName": "Claude", "key": true},
		// the models a member's client names, which the connect page lists
		model("claude/claude-sonnet-5-5", "Claude Sonnet 5.5"),
		model("claude/claude-opus-5-5", "Claude Opus 5.5"),
		model("claude/claude-haiku-5-5", "Claude Haiku 5.5"),
	}
}

// model is one entry of a provider's models, as gateway-keys/models lists
// it: an id magpie serves, under "<provider>/<model>".
func model(id, name string) map[string]any {
	p, m, _ := strings.Cut(id, "/")
	return map[string]any{"id": id, "name": name, "provider": p, "providerName": p, "model": m}
}

// claudeProviders is GET /providers as magpie sends it for three Claude
// accounts: two of team A's, one of team B's.
func claudeProviders() []map[string]any {
	login := func(user, plan string, active, on, paused bool) map[string]any {
		return map[string]any{"agent": "claude", "user": user, "plan": plan, "active": active, "on": on, "paused": paused}
	}
	return []map[string]any{{
		"id":   "claude",
		"name": "Claude",
		"account": map[string]any{
			"agent": "claude",
			"user":  "alice@example.com",
			"plan":  "Max",
			"logins": []map[string]any{
				login("alice@example.com", "Max", true, true, false),
				login("bob@example.com", "Pro", false, true, false),
				login("carol@example.com", "Pro", false, false, false),
			},
		},
	}}
}

// claudeUsage is GET /login/usage?agent=claude: team A's two accounts read,
// team B's not read at all — the case a page must say rather than draw as
// zero.
func claudeUsage() map[string]any {
	reset := time.Date(2026, 11, 18, 12, 30, 0, 0, time.UTC)
	week := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	readAt := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	return map[string]any{
		"alice@example.com": map[string]any{
			"windows": []map[string]any{
				{"name": "5 hours", "used": 42.0, "resetsAt": reset},
				{"name": "7 days", "used": 100.0, "resetsAt": week},
			},
			"readAt": readAt,
		},
		"bob@example.com": map[string]any{
			"windows": []map[string]any{
				{"name": "5 hours", "used": 0.0, "resetsAt": reset},
			},
			"readAt": readAt,
		},
	}
}

// setModelName changes how magpie shows an account, for a test about a
// name that is data rather than markup.
func setModelName(t *testing.T, f *fixture, id, name string) {
	t.Helper()
	f.magpie.mu.Lock()
	defer f.magpie.mu.Unlock()
	for _, m := range f.magpie.models {
		if m["id"] == id {
			m["name"] = name
		}
	}
}

// ops is the commands' side of the same fixture: the same state folder and
// the same magpie the portal reads, on a clock the test sets.
func (f *fixture) ops() *Ops {
	return &Ops{
		store: f.srv.Store(),
		cat:   f.magpie,
		now:   func() time.Time { return time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC) },
	}
}

// pastesTaken and canceledIDs are what magpie was told, read under its lock.
func (f *fakeMagpie) pastesTaken() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pastes...)
}

func (f *fakeMagpie) canceledIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.canceled...)
}

// groups is magpie's groups as it has them now, by group id.
func (f *fixture) groups(t *testing.T) map[string][]string {
	t.Helper()
	f.magpie.mu.Lock()
	defer f.magpie.mu.Unlock()
	out := map[string][]string{}
	for _, g := range f.magpie.groups {
		out[g.ID] = g.Accounts
	}
	return out
}

// teamsOnDisk is what teams.json holds now.
func (f *fixture) teamsOnDisk(t *testing.T) []Team {
	t.Helper()
	teams, err := f.srv.Store().Teams()
	if err != nil {
		t.Fatal(err)
	}
	return teams
}

// auditOnDisk is audit.jsonl as it stands, raw.
func (f *fixture) auditOnDisk(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.srv.Store().Dir(), auditFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

// serveSignIn is magpie's four sign-in routes, as far as the portal uses
// them: start one, read one, paste into one, cancel one.
//
// It models the one thing that matters for the boundary — the sign-in is
// global, so any caller with the admin key can read any id — and the one
// thing that matters for the flow: the account appears in magpie's account
// list only once the paste is taken.
func (f *fakeMagpie) serveSignIn(w http.ResponseWriter, r *http.Request) {
	if got, want := r.Header.Get("Authorization"), "Bearer "+testAdminKey; got != want {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, AdminPath)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && path == "signin":
		f.mu.Lock()
		on := f.onSignIn
		f.mu.Unlock()
		if on != nil {
			on(f)
		}
		f.mu.Lock()
		id := "si-" + strconv.Itoa(len(f.signIns)+1)
		st := map[string]any{
			"id": id, "agent": "claude", "state": "waiting",
			"url":           "https://claude.com/cai/oauth/authorize?code=true&client_id=x&state=y",
			"pasteCallback": true,
		}
		f.signIns[id] = st
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(st)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "signin/"):
		id := strings.TrimPrefix(path, "signin/")
		f.mu.Lock()
		st, ok := f.signIns[id]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(st)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/callback"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "signin/"), "/callback")
		var in struct{ URL string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		st, ok := f.signIns[id]
		if ok {
			f.pastes = append(f.pastes, in.URL)
		}
		on := f.onCallback
		f.mu.Unlock()
		if on != nil {
			on(f)
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		// only this sign-in's own address is taken, as magpie checks it
		if !strings.HasPrefix(in.URL, "http://localhost:") || !strings.Contains(in.URL, "code=") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"that address isn't from this sign-in: paste the one its browser tab ended on"}`)
			return
		}
		user, plan := f.signed[id], "Max"
		if user == "" {
			user = "new@example.com"
		}
		f.mu.Lock()
		st["state"], st["user"], st["plan"] = "done", user, plan
		// the account reaches magpie's lists now, as a real one does
		f.addAccount(user, "claude/"+userID(user), plan)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/cancel"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "signin/"), "/cancel")
		f.mu.Lock()
		if st, ok := f.signIns[id]; ok {
			st["state"] = "canceled"
			f.canceled = append(f.canceled, id)
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// serveLogin is magpie's POST login/{on,off}: the switch a team's lead
// reaches through the portal. It models what the real one does with the
// accounts it is given, since the portal's pages say what magpie answered:
// a saved account's On flips, the account its agent is signed in to is
// Paused instead, and turning that one off while it is the only account on
// is refused in magpie's own words.
func (f *fakeMagpie) serveLogin(w http.ResponseWriter, r *http.Request, on bool) {
	if got, want := r.Header.Get("Authorization"), "Bearer "+testAdminKey; got != want {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var in struct{ Agent, User string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	f.logins = append(f.logins, strings.TrimSpace(map[bool]string{true: "on", false: "off"}[on])+" "+in.Agent+" "+in.User)
	hook := f.onLogin
	f.mu.Unlock()
	if hook != nil {
		if msg := hook(f, on, in.User); msg != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		// a hook that answers "" took the call and changed nothing, as a
		// vendor-rested account does: magpie's own change is not made
		f.mu.Lock()
		found := false
		for _, l := range f.claudeLogins() {
			if strings.EqualFold(fmt.Sprint(l["user"]), in.User) {
				found = true
			}
		}
		f.mu.Unlock()
		if !found {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("no saved %s account %q", in.Agent, in.User)})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	logins := f.claudeLogins()
	live := -1
	for i, l := range logins {
		if l["active"] == true {
			live = i
		}
	}
	target := -1
	for i, l := range logins {
		if strings.EqualFold(fmt.Sprint(l["user"]), in.User) {
			target = i
		}
	}
	if target < 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("no saved %s account %q", in.Agent, in.User)})
		return
	}
	if target == live {
		if !on {
			others := false
			for i, l := range logins {
				if i != target && l["on"] == true {
					others = true
				}
			}
			if !others {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": in.User + " is the only " + in.Agent + " account in use; turn another on first to pause it",
				})
				return
			}
		}
		logins[target]["paused"] = !on
		logins[target]["on"] = true
	} else {
		logins[target]["on"] = on
		logins[target]["paused"] = false
	}
	w.WriteHeader(http.StatusNoContent)
}

// claudeLogins is the provider's logins list, which is where magpie's own
// switch acts. The caller holds f.mu.
func (f *fakeMagpie) claudeLogins() []map[string]any {
	if len(f.providers) == 0 {
		f.providers = claudeProviders()
	}
	acct := f.providers[0]["account"].(map[string]any)
	logins, _ := acct["logins"].([]map[string]any)
	return logins
}

// loginsTaken are the login/{on,off} calls magpie took, in order.
func (f *fakeMagpie) loginsTaken() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logins...)
}

// setLogins replaces the accounts magpie remembers for Claude, each as
// "user:on:paused:active".
func (f *fakeMagpie) setLogins(t *testing.T, rows ...string) {
	t.Helper()
	var logins []map[string]any
	for _, row := range rows {
		parts := strings.Split(row, ":")
		if len(parts) != 4 {
			t.Fatalf("a login row is user:on:paused:active, not %q", row)
		}
		on, paused, active := parts[1] == "on", parts[2] == "paused", parts[3] == "active"
		logins = append(logins, map[string]any{
			"agent": "claude", "user": parts[0], "plan": "Pro",
			"active": active, "on": on, "paused": paused,
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claudeLogins()
	f.providers[0]["account"].(map[string]any)["logins"] = logins
}

// accountNames is GET /providers' AccountNames, replaced whole.
func (f *fakeMagpie) setAccountNames(t *testing.T, names map[string]string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claudeLogins()
	f.providers[0]["accountNames"] = names
}

// serveModels is GET /v1/models, the gateway's own list: what a member's
// client gets, and what the connect page asks for with the team's key.
//
// It takes the key a member holds, not the admin key, and it lists only
// what that key may use — which is the point of the page asking here rather
// than being told by the management API.
func (f *fakeMagpie) serveModels(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	var held []string
	found := false
	for _, k := range f.keys {
		if k.ID == key || key == f.keySecret(k.ID) {
			held, found = k.Models, true
			break
		}
	}
	models := f.models
	refuse := f.refuseModels
	f.mu.Unlock()
	if !found {
		// the same answer magpie gives a key it doesn't know
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"this key isn't one magpie knows"}}`)
		return
	}
	if refuse != 0 {
		w.WriteHeader(refuse)
		fmt.Fprint(w, `{"error":{"message":"refused"}}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var data []map[string]any
	for _, m := range models {
		id, _ := m["id"].(string)
		if m["key"] == true {
			continue
		}
		if len(held) > 0 && !allowedModel(held, id) {
			continue
		}
		data = append(data, map[string]any{"id": id, "object": "model"})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

// keySecret is the secret a fake key is used with: the id is what ops
// handles, and a member holds the secret. A test sets it with setKeySecret.
func (f *fakeMagpie) keySecret(id string) string {
	if s, ok := f.secrets[id]; ok {
		return s
	}
	return "sk-magpie-key-" + id
}

// setKeySecret gives a key a secret for GET /v1/models, and sets what it is
// held to.
func (f *fakeMagpie) setKey(t *testing.T, id, secret string, models []string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secrets == nil {
		f.secrets = map[string]string{}
	}
	f.secrets[id] = secret
	for i := range f.keys {
		if f.keys[i].ID == id {
			f.keys[i].Models = models
		}
	}
}

// addAccount puts an account in magpie's lists, as a finished sign-in does.
// The caller holds f.mu.
func (f *fakeMagpie) addAccount(user, id, plan string) {
	f.models = append(f.models, map[string]any{
		"id": id, "name": user, "provider": "claude", "providerName": "Claude", "account": true, "plan": plan,
	})
	if len(f.providers) == 0 {
		f.providers = claudeProviders()
	}
	acct := f.providers[0]["account"].(map[string]any)
	logins, _ := acct["logins"].([]map[string]any)
	acct["logins"] = append(logins, map[string]any{
		"agent": "claude", "user": user, "plan": plan, "active": false, "on": true, "paused": false,
	})
}

// userID is the stable id magpie would give an account, for a fake: any
// fixed 16 hex characters, different for each user.
func userID(user string) string {
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:8])
}

// pasteFor is the address a browser of this sign-in would end on.
func pasteFor(code string) string {
	return "http://localhost:54545/callback?code=" + code + "&state=xyz"
}

// AccountGroups and GatewayKeys are Magpie's reading of the fake: the
// state the test set, never the HTTP routes, so ops and the portal see one
// magpie.
func (f *fakeMagpie) AccountGroups(context.Context) ([]Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "GET "+AdminPath+"account-groups")
	if f.status != 0 {
		return nil, fmt.Errorf("magpie answered %d", f.status)
	}
	return append([]Group(nil), f.groups...), nil
}

func (f *fakeMagpie) GatewayKeys(context.Context) ([]GatewayKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "GET "+AdminPath+"gateway-keys")
	if f.status != 0 {
		return nil, fmt.Errorf("magpie answered %d", f.status)
	}
	return append([]GatewayKey(nil), f.keys...), nil
}
