package teamd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The connect page: what a team's lead sends to its members so they can
// reach the team's pool without asking anyone.
//
// It carries the one secret the portal ever shows — the team's gateway
// key, when ops mounted it — and it is a page like the rest: no script, no
// request to anywhere, and no-referrer. The key is printed as text a
// member copies, never put in a URL, a query string or a link.
//
// The page is useful without the key. Ops may keep it out of teamd on
// purpose (a Secret the team reads where it lives, rather than one this
// process can print), and a member who has it from somewhere else still
// needs the address, the settings and the check.

// ConnectURL is the address members reach the gateway at. It is a config
// value and not something read from the request: the portal is reached
// through Istio at one name, and magpie's gateway at another, and which
// header says what depends on the proxy in front. Naming it once is the
// only way the page can be right about it.
type ConnectURLs struct {
	// Gateway is the base URL a member's client calls, without a trailing
	// slash: https://gateway.example.com.
	Gateway string
	// Models are the model ids a member may name. Empty means the portal
	// has none to show — magpie couldn't be asked — and the page says so
	// rather than printing an empty list.
	Models []string
}

// KeyDir is the folder a team's gateway key may be mounted in, under the
// state folder by default: one file per team, named for its id, as
// <dir>/<team-id>.key.
//
// It is a folder and not a file because a portal serves more than one team,
// and one file would print the same key on every team's page — a member of
// one team would be handed another team's key, and the pool it reaches.
const KeyDir = "keys"

// keyPath is where a team's key may be mounted: <dir>/<team-id>.key.
func (s *Server) keyPath(teamID string) string {
	dir := strings.TrimSpace(s.cfg.KeyFile)
	if dir == "" {
		dir = filepath.Join(s.cfg.StateDir, KeyDir)
	}
	// a team id is letters, digits and "-" (teamID), so it is a file name
	// nothing can climb out of the folder with
	return filepath.Join(dir, teamID+".key")
}

// teamKey is the team's gateway key as the page may show it, and whether
// it was there to read. A file that can't be read is not an empty key: the
// page says the key couldn't be read rather than inviting a member to copy
// nothing.
func (s *Server) teamKey(teamID string) (key string, state keyState) {
	b, err := os.ReadFile(s.keyPath(teamID))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", keyAbsent
	case err != nil:
		return "", keyUnreadable
	}
	key = strings.TrimSpace(string(b))
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		// one key, on one line: a file holding more is refused rather than
		// guessed at, as the admin key file is
		return "", keyUnreadable
	}
	return key, keyPresent
}

// keyState is whether the page has the team's key to show.
type keyState int

const (
	// keyPresent: the key is mounted and the page prints it.
	keyPresent keyState = iota
	// keyAbsent: ops kept it out of teamd, as the page expects.
	keyAbsent
	// keyUnreadable: a file is there and can't be read as one key.
	keyUnreadable
)

// Connect is the page's data.
type Connect struct {
	Team    Team
	Token   string
	Gateway string
	// Key is the team's gateway key, "" when the page doesn't have it;
	// KeyState says which of the three cases that is.
	Key      string
	KeyState keyState
	// Models are the ids a member may name, prefixed as magpie lists them.
	Models []string
	// ModelsErr is why there are none to show. ModelsPartial says the list
	// is not the whole one: the key is held to a routing group, which only
	// the gateway expands.
	ModelsErr     string
	ModelsPartial bool
	// GatewayErr says the address isn't configured, which makes the whole
	// page unusable: a snippet with no address is worse than none.
	GatewayErr string
	// Now is when the page was drawn, for the "read" line.
	Now time.Time
	// Trans is the language the page is drawn in.
	Trans trans
}

// HasKey says the page can print the key, so its snippets are complete.
func (c Connect) HasKey() bool { return c.KeyState == keyPresent && c.Key != "" }

// KeyNote is what the page says about the key it has or hasn't.
func (c Connect) KeyNote(t trans) string {
	switch c.KeyState {
	case keyPresent:
		return ""
	case keyUnreadable:
		return t.T("connect.keynote.unreadable")
	default:
		return t.T("connect.keynote.absent")
	}
}

// usageModel is the model a member's client should name: the prefixed id,
// which magpie lists and which works whether or not the client prefixes
// its own.
func connectModels(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}

// ConnectFor is the page's data from the team's own grant.
//
// The models are magpie's list for this team's key — the admin API's
// gateway-keys/models, which is the same answer the key's own /v1/models
// is built from — so the page names what the team may actually use. Only
// this team's are shown: the answer covers every key, and what reaches the
// page is the ones the key's models allow.
func (s *Server) ConnectFor(ctx context.Context, g Grant, t trans) (Connect, error) {
	page := Connect{Trans: t, Team: g.Team(), Gateway: s.gatewayURL(), Now: s.now()}
	if page.Gateway == "" {
		page.GatewayErr = t.T("connect.err.gateway")
	}
	page.Key, page.KeyState = s.teamKey(g.Team().ID)

	// the models this team's key may use: the gateway's own answer when the
	// key is mounted, which is what a member's client gets, else magpie's
	// management API filtered here
	mounted := ""
	if page.HasKey() {
		mounted = page.Key
	}
	ids, complete, err := s.ModelsFor(ctx, g.Team().KeyID, mounted)
	switch {
	case errors.Is(err, ErrKeyRefused):
		page.ModelsErr = t.T("connect.err.refused")
		return page, nil
	case err != nil:
		page.ModelsErr = t.T("connect.err.models")
		return page, nil
	}
	page.Models = connectModels(ids)
	if !complete {
		page.ModelsPartial = true
	}
	return page, nil
}

// gatewayURL is the address members reach, without a trailing slash.
func (s *Server) gatewayURL() string {
	return strings.TrimRight(strings.TrimSpace(s.cfg.GatewayURL), "/")
}

// connectRoutes is the connect page's route.
func (s *Server) connectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /team/{token}/connect", s.serveConnect)
}

// serveConnect answers GET /team/{token}/connect.
func (s *Server) serveConnect(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	t := transOf(r)
	page, err := s.ConnectFor(ctx, g, t)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	s.render(w, t, http.StatusOK, connectPage(page))
}

// snippetEnv is the settings a member's shell needs, as the page shows
// them: the address, and the key where the page has it.
//
// With no address configured there is no snippet at all. A block that
// exports an empty ANTHROPIC_BASE_URL would send a member's requests to
// Anthropic's own endpoint with a magpie key — the one outcome worse than
// an empty page.
//
// ANTHROPIC_AUTH_TOKEN is what Claude Code sends as its bearer token, and
// ANTHROPIC_API_KEY what other Anthropic-shaped clients send. Magpie takes
// either; the page sets the first, which Claude Code prefers, and names
// the second in the note.
func snippetEnv(c Connect) string {
	if c.Gateway == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("export ANTHROPIC_BASE_URL=" + c.Gateway + "\n")
	b.WriteString("export ANTHROPIC_AUTH_TOKEN=" + keyOrPlaceholder(c) + "\n")
	return b.String()
}

// snippetJSON is the same settings in the file Claude Code reads, so a
// member who would rather not export anything can paste one file.
func snippetJSON(c Connect) string {
	if c.Gateway == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`  "env": {` + "\n")
	b.WriteString(`    "ANTHROPIC_BASE_URL": "` + c.Gateway + `",` + "\n")
	b.WriteString(`    "ANTHROPIC_AUTH_TOKEN": "` + keyOrPlaceholder(c) + `"` + "\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// keyOrPlaceholder is the key, or the word a member replaces.
func keyOrPlaceholder(c Connect) string {
	if c.HasKey() {
		return c.Key
	}
	return "TEAM_KEY"
}

// snippetCheck is the one-line check a member runs to prove it works.
//
// It asks for the model list, which is the smallest answer the gateway
// gives and needs nothing but the key. The expected answer is the first
// model's id: a list that comes back at all proves the address, the key
// and the route in one go, and naming one model says what "worked" looks
// like rather than leaving a member to read JSON.
func snippetCheck(c Connect) string {
	if c.Gateway == "" {
		return ""
	}
	return "curl -sS " + c.Gateway + "/v1/models \\\n" +
		"  -H \"Authorization: Bearer " + keyOrPlaceholder(c) + "\""
}

// expectedAnswer is what the check prints when it works.
func expectedAnswer(c Connect) string {
	if len(c.Models) == 0 {
		return `{"object":"list","data":[{"id":"…","object":"model"}, …]}`
	}
	return `{"object":"list","data":[{"id":"` + c.Models[0] + `","object":"model"}, …]}`
}

// checkModel is the model a member's client names: the first one this
// team's key may use, so the snippet works as printed.
func checkModel(c Connect) string {
	if len(c.Models) == 0 {
		return "claude-sonnet-5-5"
	}
	// magpie lists "<provider>/<model>"; a client naming the bare model is
	// answered the same, and the bare form reads better in a snippet
	if _, m, ok := strings.Cut(c.Models[0], "/"); ok && strings.Contains(c.Models[0], "claude") {
		return m
	}
	return c.Models[0]
}
