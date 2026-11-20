package gui

import (
	"context"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/teamd"
)

// The team portal's ops page, served by magpie's own web page: the one
// place a team and its link come into being.
//
// It runs in magpie's process, over teamd's files and magpie's own groups
// and keys, with no key of its own: the page is already behind magpie
// web's sign-in, and a portal's admin key exists only for the portal's
// HTTP calls. It is not under /api, so the management API never reaches
// it, and a guest of the app's windows (a phone, a box) never sees it.

// guiMagpie is magpie's own reading of what ops asks, in process.
type guiMagpie struct{}

func (guiMagpie) AccountGroups(context.Context) ([]teamd.Group, error) {
	gs, err := access.AccountGroups()
	if err != nil {
		return nil, err
	}
	out := make([]teamd.Group, 0, len(gs))
	for _, g := range gs {
		out = append(out, teamd.Group{ID: g.ID, Name: g.Name, Accounts: g.Accounts})
	}
	return out, nil
}

func (guiMagpie) GatewayKeys(context.Context) ([]teamd.GatewayKey, error) {
	keys, err := access.List()
	if err != nil {
		return nil, err
	}
	out := make([]teamd.GatewayKey, 0, len(keys))
	for _, k := range keys {
		// Accounts travels with the key: Ops.Add reads it to know whether
		// the key is already held to its group, which without it looks like
		// no key is (a field left out here is the check reading an empty
		// list, and the page holding every key again).
		out = append(out, teamd.GatewayKey{ID: k.ID, Name: k.Name, Off: k.Off, Accounts: k.Accounts, Issues: k.Issues, Admin: k.Admin})
	}
	return out, nil
}

// HoldKeyToGroup is accounts-key in this process: the same call the
// Gateway page's own key menu makes, with no key and no HTTP.
func (guiMagpie) HoldKeyToGroup(_ context.Context, keyID, groupID string) error {
	_, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: []string{"@" + strings.TrimSpace(groupID)}})
	return err
}

// opsRoutes are the ops page's routes. The store is teamd's own folder on
// the volume, opened here with the same lock teamd takes.
func opsRoutes(store *teamd.Store, magpie teamd.Magpie) http.Handler {
	ops := teamd.NewOpsWith(store, magpie)
	// what the page's forms and tables draw: the teams, and magpie's own
	// groups and keys to pick from
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ops/teams", func(w http.ResponseWriter, r *http.Request) {
		drawOps(w, r, ops, nil)
	})
	mux.HandleFunc("POST /ops/teams/add", func(w http.ResponseWriter, r *http.Request) {
		if !opsOwnPage(r) {
			http.Error(w, "this form is submitted from magpie's own page", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "the form didn't read", http.StatusBadRequest)
			return
		}
		ctx, cancel := requestContext(r)
		defer cancel()
		team, err := ops.Add(ctx, teamd.NewTeam{
			Name:    strings.TrimSpace(r.PostFormValue("name")),
			GroupID: strings.TrimSpace(r.PostFormValue("group")),
			KeyID:   strings.TrimSpace(r.PostFormValue("key")),
		})
		if err != nil {
			drawOps(w, r, ops, &opsNotice{Err: err.Error()})
			return
		}
		token, _, err := ops.Token(team.ID)
		if err != nil {
			drawOps(w, r, ops, &opsNotice{Err: "the team was made, but its link couldn't be: " + err.Error()})
			return
		}
		drawOps(w, r, ops, &opsNotice{Link: "/team/" + token, Team: team})
	})
	mux.HandleFunc("POST /ops/teams/token", func(w http.ResponseWriter, r *http.Request) {
		if !opsOwnPage(r) {
			http.Error(w, "this form is submitted from magpie's own page", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "the form didn't read", http.StatusBadRequest)
			return
		}
		token, team, err := ops.Token(strings.TrimSpace(r.PostFormValue("id")))
		if err != nil {
			drawOps(w, r, ops, &opsNotice{Err: err.Error()})
			return
		}
		drawOps(w, r, ops, &opsNotice{Link: "/team/" + token, Team: team,
			Note: "The team's previous link stops working now."})
	})
	mux.HandleFunc("POST /ops/teams/remove", func(w http.ResponseWriter, r *http.Request) {
		if !opsOwnPage(r) {
			http.Error(w, "this form is submitted from magpie's own page", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "the form didn't read", http.StatusBadRequest)
			return
		}
		ctx, cancel := requestContext(r)
		defer cancel()
		gone, err := ops.Remove(ctx, strings.TrimSpace(r.PostFormValue("id")), r.PostFormValue("force") != "")
		if err != nil {
			drawOps(w, r, ops, &opsNotice{Err: err.Error()})
			return
		}
		note := "The team is gone. Its account group and gateway key are untouched."
		if n := len(gone.Accounts()); n > 0 {
			note = "The team is gone, but its group still holds " + itoa(n) + " accounts, and its key still serves them: neither was changed."
		}
		drawOps(w, r, ops, &opsNotice{Note: note})
	})
	return mux
}

// opsOwnPage turns away a write another site's page sent: the page's
// cookie is SameSite=Lax, so a sibling name behind the same proxy could
// send one, as webOwnPage says of every write of the app's.
func opsOwnPage(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == "http://"+r.Host || r.Header.Get("Origin") == "https://"+r.Host
}

// requestContext bounds one action the way the portal's own do.
func requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

// opsView is what the page draws.
type opsView struct {
	Teams     []teamd.Row
	Groups    []teamd.Group
	GroupsErr string
	Keys      []teamd.GatewayKey
	KeysErr   string
	Notice    *opsNotice
	Err       string
	Host      string
}

// opsNotice is what the last action did, or why it didn't.
type opsNotice struct {
	// Link is a token's one showing: the path, which the page draws with
	// the address it was reached at.
	Link string
	// Team is whose link it is.
	Team teamd.Team
	Err  string
	Note string
}

// drawOps draws the page: the teams, the form to add one, and what the
// last action did.
func drawOps(w http.ResponseWriter, r *http.Request, ops *teamd.Ops, notice *opsNotice) {
	ctx, cancel := requestContext(r)
	defer cancel()
	rows, err := ops.List(ctx)
	groups, gErr := ops.Groups(ctx)
	keys, kErr := ops.Keys(ctx)
	view := opsView{
		Teams: rows, Notice: notice,
		Groups: groups, GroupsErr: errorText(gErr),
		Keys: keys, KeysErr: errorText(kErr),
		Err:  errorText(err),
		Host: r.Host,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(renderOps(view)))
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// renderOps is the page's markup. It has no script: a link is shown as
// text to copy, and every action is a form.
func renderOps(v opsView) string {
	var b strings.Builder
	b.WriteString(opsHead)
	b.WriteString(`<h1>Team portal</h1>`)
	if v.Err != "" {
		b.WriteString(`<p class="bad">` + template.HTMLEscapeString(v.Err) + `</p>`)
	}
	if n := v.Notice; n != nil {
		if n.Err != "" {
			b.WriteString(`<p class="bad">` + template.HTMLEscapeString(n.Err) + `</p>`)
		} else {
			if n.Note != "" {
				b.WriteString(`<p class="ok">` + template.HTMLEscapeString(n.Note) + `</p>`)
			}
			if n.Link != "" {
				b.WriteString(`<h2>` + template.HTMLEscapeString(n.Team.Name) + `'s link</h2>
<p class="note">This is the only time it is shown. Give it to the team's lead; it is what they sign in with.</p>
<p class="link">https://` + template.HTMLEscapeString(v.Host) + template.HTMLEscapeString(n.Link) + `</p>`)
			}
		}
	}

	b.WriteString(`<h2>Teams</h2>`)
	if len(v.Teams) == 0 {
		b.WriteString(`<p class="note">No teams yet. Make one below.</p>`)
	} else {
		b.WriteString(`<table><tr><th>Team</th><th>Group</th><th>Key</th><th>Link</th><th>Accounts</th><th></th></tr>`)
		for _, t := range v.Teams {
			link := "not issued"
			if t.Team.TokenHash != "" {
				link = "live"
			}
			accounts := "?"
			if t.Known {
				accounts = itoa(t.Accounts)
			}
			id := template.HTMLEscapeString(t.Team.ID)
			name := template.HTMLEscapeString(t.Team.Name)
			b.WriteString(`<tr><td>` + name + `</td><td class="mono">` + template.HTMLEscapeString(t.Team.GroupID) +
				`</td><td class="mono">` + template.HTMLEscapeString(t.Team.KeyID) + `</td><td>` + link +
				`</td><td>` + accounts + `</td><td>
<form method="post" action="/ops/teams/token"><input type="hidden" name="id" value="` + id + `"><button>New link</button></form>
<form method="post" action="/ops/teams/remove"><input type="hidden" name="id" value="` + id + `"><input type="hidden" name="force" value="1"><button class="q">Remove</button></form>
</td></tr>`)
		}
		b.WriteString(`</table>`)
	}

	b.WriteString(`<h2>Make a team</h2>
<p class="note">Make its account group and its gateway key on the Gateway page first, then pick them here. The team's lead adds the accounts.</p>
<form method="post" action="/ops/teams/add" class="add">
<label>Name <input name="name" required maxlength="80" placeholder="team-c"></label>
<label>Account group <select name="group" required>`)
	if v.GroupsErr != "" {
		b.WriteString(`<option value="">` + template.HTMLEscapeString(v.GroupsErr) + `</option>`)
	}
	for _, g := range v.Groups {
		b.WriteString(`<option value="` + template.HTMLEscapeString(g.ID) + `">` + template.HTMLEscapeString(g.Name) + ` · ` + itoa(len(g.Accounts)) + ` accounts</option>`)
	}
	b.WriteString(`</select></label>
<label>Gateway key <select name="key" required>`)
	if v.KeysErr != "" {
		b.WriteString(`<option value="">` + template.HTMLEscapeString(v.KeysErr) + `</option>`)
	}
	for _, k := range v.Keys {
		if k.Off || k.Admin != "" {
			continue // a key that manages magpie is never a team's
		}
		issuing := ""
		if !k.Issues {
			issuing = ` · can't issue member keys yet`
		}
		b.WriteString(`<option value="` + template.HTMLEscapeString(k.ID) + `">` + template.HTMLEscapeString(k.Name) + issuing + `</option>`)
	}
	b.WriteString(`</select></label>
<button>Make the team and its link</button>
</form>
<p class="note">The link is shown once. If it is lost, make a new one: the old stops working.</p>
</body></html>`)
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

const opsHead = `<!doctype html>
<html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Team portal</title>
<style>
body{font:13px -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;max-width:56rem;margin:2rem auto;padding:0 1rem;color:#1c1c21;background:#f4f4f6}
h1{font-size:18px;margin:0 0 1rem}h2{font-size:14px;margin:1.6rem 0 .5rem}
table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid #e3e3e8;vertical-align:middle}
th{font-size:11px;color:#85858d;font-weight:600}
.mono{font-family:ui-monospace,Menlo,monospace;font-size:11px}
.note{color:#85858d;font-size:11.5px}.ok{color:#189a58}.bad{color:#d64545}
.link{font-family:ui-monospace,Menlo,monospace;font-size:12px;background:#fff;border:1px solid #e3e3e8;border-radius:7px;padding:8px 10px;overflow-wrap:anywhere}
form{display:inline}button{font:inherit;font-size:11.5px;padding:3px 9px;border:1px solid #e3e3e8;background:#f1f1f4;border-radius:6px;cursor:pointer;margin-right:4px}
button.q{color:#85858d}.add{display:flex;flex-wrap:wrap;gap:10px;align-items:flex-end;margin-top:8px}
.add label{display:flex;flex-direction:column;gap:4px;font-size:11px;color:#4b4b53}
.add input,.add select{font:inherit;font-size:12px;padding:4px 7px;border:1px solid #e3e3e8;border-radius:6px;background:#fff;min-width:12rem}
@media (prefers-color-scheme: dark){body{background:#1a1a1e;color:#ededf1}th,td{border-color:#333339}.link,.add input,.add select{background:#232327;border-color:#333339}button{background:#2d2d33;border-color:#333339;color:#ededf1}.note{color:#8b8b94}}
</style></head><body>
`

// teamdOpsDir is where teamd keeps its teams, as the sidecar was given it:
// the same folder on the volume, so the page and the portal read one file.
// MAGPIE_TEAMD_DIR names another, as a run outside the container has.
func teamdOpsDir() string {
	if d := strings.TrimSpace(os.Getenv("MAGPIE_TEAMD_DIR")); d != "" {
		return d
	}
	return filepath.Join(settings.Dir(), "teamd")
}
