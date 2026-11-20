package teamd

import (
	"cmp"
	"context"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The accounts page: everything a team's lead sees at /team/<token>.
//
// What it draws is chosen here, on the server, from one team's group. The
// answers it is drawn from (GET /providers, GET /gateway-keys) name every
// account of every team, and neither leaves this process: the page is
// built from the accounts this team's group holds. An entry the group
// holds that names nothing magpie has now is shown as such, never
// dropped — a group listing an account signed out since is a thing its
// lead needs to see.

// maxAccountsOnPage bounds what one page draws. A group past it is cut and
// the page says how many it left out: a page a browser stalls on serves
// nobody.
const maxAccountsOnPage = 200

// agentClaude is the agent whose allowances the portal reads. The pool it
// serves is Claude's; a second agent's pool is a change to this line and
// to the page's words, not a new shape.
const agentClaude = "claude"

// AccountRow is one account as the page shows it.
type AccountRow struct {
	// Entry is the group's entry, "<provider>/<stable id>".
	Entry string
	// Name is how the account is shown, "" for an entry that names no
	// account magpie has now.
	Name string
	Plan string
	// ProviderName is the provider's name, "Claude".
	Provider     string
	ProviderName string
	// Known is false for an entry magpie has no account for: the group
	// still lists it, and it matches nothing. Key is one of a provider's
	// API keys, which has no allowance of its own to show.
	Known bool
	Key   bool
	// On is whether magpie serves from it; Off whether it was disabled;
	// Paused whether its agent is signed in to it while another serves.
	On     bool
	Off    bool
	Paused bool
	// SignedIn is whether magpie has a sign-in of this account, which is
	// what the disable and enable controls act on.
	SignedIn bool
	// Others is how many of the team's other accounts magpie serves from:
	// none, and disabling this one empties the pool.
	Others int
	// Reading is the account's allowance as magpie last read it, nil when
	// magpie has no reading.
	Reading *Reading
	// Note says why a window is missing or old, in the page's words.
	Note string
}

// Accounts is what the page draws: one team, its group, and the accounts
// that group holds.
type Accounts struct {
	// Trans is the language the page is drawn in, which the template reads
	// the page's sentences from.
	Trans trans
	Team  Team
	Group Group
	// Rows are the group's accounts, in use first then by name, cut to
	// maxAccountsOnPage; More is how many were left out.
	Rows []AccountRow
	More int
	// On counts the team's accounts magpie serves from now, and Total the
	// accounts the group holds that magpie still has (keys and lost
	// entries are not accounts).
	On    int
	Total int
	// ReadAt is the newest of the readings shown, nil when none was made.
	ReadAt *time.Time
	// Now is the clock the page's ages are counted from.
	Now time.Time
	// Token is the team's link's token, for the page's own links.
	Token string
	// Added is the account a sign-in just put in the pool, "" when none
	// just was.
	Added string
	// Notice is what the last switch did, "" when this draw isn't the
	// answer to one.
	Notice switchNotice
}

// AccountsFor is the page's data: the boundary's grant, then the accounts
// of that group only, each with its allowance.
//
// This is the one place a team's accounts are chosen. What it reads from
// magpie is filtered here, and what it answers holds nothing of another
// team. The grant it takes is the boundary's: there is no way to call it
// for a group the token didn't resolve to.
func (s *Server) AccountsFor(ctx context.Context, g Grant, t trans) (Accounts, error) {
	catalog, err := s.admin.Catalog(ctx)
	if err != nil {
		return Accounts{}, err
	}
	usage, usageErr := s.admin.Usage(ctx, agentClaude)
	page := Accounts{Trans: t, Team: g.Team(), Group: g.Group(), Now: time.Now()}
	entries := g.Accounts()
	if len(entries) > maxAccountsOnPage {
		page.More = len(entries) - maxAccountsOnPage
		entries = entries[:maxAccountsOnPage]
	}
	for _, entry := range entries {
		row := AccountRow{Entry: entry}
		acct, known := catalog.Accounts[strings.ToLower(entry)]
		switch {
		case !known:
			// an entry signed out since, or an id magpie no longer keeps:
			// it stays in the group and matches nothing
			row.Note = t.T("accounts.note.lost")
		case acct.Key:
			row.Known, row.Key, row.Name = true, true, acct.Name
			row.Plan = acct.Plan
			row.ProviderName = cmp.Or(acct.ProviderName, providerOf(entry))
			row.Note = t.T("accounts.note.key")
		default:
			row.Known = true
			row.Name, row.Plan = cmp.Or(acct.Name, acct.User), acct.Plan
			row.ProviderName = cmp.Or(acct.ProviderName, providerOf(entry))
			row.On, row.Off, row.Paused, row.SignedIn = acct.On, acct.Off, acct.Paused, acct.SignedIn
			page.Total++
			if row.On {
				page.On++
			}
			switch {
			case usageErr != nil:
				row.Note = t.T("accounts.note.no_allowance")
			default:
				r, ok := usage[accountKey(acct.User)]
				if !ok {
					row.Note = t.T("accounts.note.no_reading")
					break
				}
				row.Reading = &r
				if r.ReadAt != nil && (page.ReadAt == nil || r.ReadAt.After(*page.ReadAt)) {
					page.ReadAt = r.ReadAt
				}
				row.Note = readingNote(t, r)
			}
		}
		page.Rows = append(page.Rows, row)
	}
	// the accounts that can serve now first, then by name: a pool's lead
	// looks for room, and a disabled account is not room
	sort.SliceStable(page.Rows, func(i, j int) bool {
		if page.Rows[i].On != page.Rows[j].On {
			return page.Rows[i].On
		}
		return strings.ToLower(page.Rows[i].Name) < strings.ToLower(page.Rows[j].Name)
	})
	// what each row's disable would leave behind: a page drawn before the
	// click is where the warning has to be, since the click itself is one
	// post and nothing on the page can ask first
	for i := range page.Rows {
		if page.Rows[i].On {
			page.Rows[i].Others = page.On - 1
		}
	}
	return page, nil
}

// providerOf is the provider id an entry names.
func providerOf(entry string) string {
	p, _, _ := strings.Cut(entry, "/")
	return p
}

// readingNote is what the page says under a reading, "" when there is
// nothing to explain.
func readingNote(t trans, r Reading) string {
	switch {
	case r.Deferred && r.AsOf != nil:
		return t.T("reading.deferred.at", argsOf{"At": r.AsOf.Local().Format("15:04")})
	case r.Deferred:
		return t.T("reading.deferred")
	case r.AsOf != nil:
		return t.T("reading.stale", argsOf{"At": r.AsOf.Local().Format("15:04")})
	case r.Error != "":
		return r.Error
	}
	return ""
}

// serveAccounts answers GET /team/{token}.
func (s *Server) serveAccounts(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	page, err := s.AccountsFor(ctx, g, transOf(r))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	// ?added=<entry> is the sign-in that just finished saying so. It is a
	// group entry, and it is shown only if this team's group holds it — a
	// name in a URL never puts anything on the page.
	if entry := r.URL.Query().Get("added"); entry != "" && slicesContainsFold(page.Group.Accounts, strings.ToLower(entry)) {
		page.Added = entry
	}
	s.render(w, transOf(r), http.StatusOK, accountsPage(transOf(r), page))
}

// authorizePage runs the boundary for a page request and answers the
// refusal itself.
//
// A token naming no team is 404 with no hint why. A team whose group
// magpie doesn't have is 404 too: its link opens nothing until ops makes
// the group. Anything else is 503 — that is magpie being unreadable, not
// the link being wrong, and the two must not look alike.
func (s *Server) authorizePage(w http.ResponseWriter, r *http.Request) (Grant, bool) {
	g, err := s.Authorize(r.Context(), r.PathValue("token"), Target{})
	switch {
	case err == nil:
		return g, true
	case errors.Is(err, ErrNoTeam), errors.Is(err, ErrNoGroup):
		s.notFound(w, r)
	case errors.Is(err, ErrNotYours):
		http.Error(w, transOf(r).T("signin.err.not_yours"), http.StatusForbidden)
	default:
		s.pageError(w, r, err)
	}
	return Grant{}, false
}

// pageError answers a page whose data couldn't be read. magpie's own words
// are not shown: an error it sends can name accounts of other teams.
func (s *Server) pageError(w http.ResponseWriter, r *http.Request, err error) {
	t := transOf(r)
	s.render(w, t, http.StatusServiceUnavailable, errorPage(t))
}

// render writes a page with the headers every page of the portal carries.
//
// The language is here and not in every caller: it comes from the request's
// Accept-Language, and no page says otherwise. It also sets Content-Language,
// which is what a caching proxy and a browser's reader mode read a page's
// language from when the markup's own lang is not enough.
func (s *Server) render(w http.ResponseWriter, t trans, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Language", t.HTML())
	// The token is in the path, and there is nothing here for a script to
	// do: no page loads anything from another origin, runs script of its
	// own, or may be framed. A frame is one way a page's URL is read. An
	// image is a data: URL or nothing: the icon is in the page itself
	// (brand.go), and data: reaches no origin.
	h.Set("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(withIcon(body, s.icon, s.favicon)))
}

// windowRow is one allowance window as the page draws it.
type windowRow struct {
	Name     string
	Share    float64 // percent used
	Display  string
	Text     bool // words, not a share
	Resets   string
	ResetsIn string
}

// windows turns a reading into the rows the page shows: the vendor's
// windows in its own order, each with its share and when it starts again.
func windows(t trans, r *Reading, now time.Time) []windowRow {
	if r == nil {
		return nil
	}
	out := make([]windowRow, 0, len(r.Windows))
	for _, w := range r.Windows {
		row := windowRow{Name: w.Name, Share: w.Used, Display: w.Display, Text: w.Text || w.Lane != "" || w.Unlimited}
		if w.ResetsAt != nil {
			row.Resets = w.ResetsAt.Local().Format("01-02 15:04")
			row.ResetsIn = until(t, now, *w.ResetsAt)
		}
		out = append(out, row)
	}
	return out
}

// Empty says the window is used up, for the bar's colour.
func (w windowRow) Empty() bool { return !w.Text && w.Share >= 100 }

// ShareInt is the bar's width, a whole percent, never over 100 nor under
// 0 whatever the vendor sends.
func (w windowRow) ShareInt() int {
	switch {
	case w.Text:
		return 0
	case w.Share <= 0:
		return 0
	case w.Share >= 100:
		return 100
	default:
		return int(w.Share + 0.5)
	}
}

// until is how long from now to at, in the page's words: "2h 10m", "3d".
func until(t trans, now, at time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return t.T("time.now")
	}
	switch {
	case d < time.Minute:
		return t.T("time.under_minute")
	case d < time.Hour:
		return t.T("time.minutes", argsOf{"N": int(d.Minutes())})
	case d < 24*time.Hour:
		return t.T("time.hours", argsOf{"H": int(d.Hours()), "M": int(d.Minutes()) % 60})
	default:
		return t.T("time.days", argsOf{"D": int(d.Hours()) / 24, "H": int(d.Hours()) % 24})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ago is when something was read, in the page's words.
func ago(t trans, now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case d < 0, d < time.Minute:
		return t.T("ago.just_now")
	case d < time.Hour:
		return t.T("ago.minutes", argsOf{"N": int(d.Minutes())})
	case d < 24*time.Hour:
		return t.T("ago.hours", argsOf{"N": int(d.Hours())})
	default:
		return t.T("ago.days", argsOf{"N": int(d.Hours()) / 24})
	}
}

// errorPage is what a page says when magpie couldn't be read: enough to
// tell ops what to look at, and nothing magpie said itself.
func errorPage(t trans) string {
	return render(template.Must(template.New("error").Funcs(t.funcs()).Parse(errorHTML)), errorView{Trans: t})
}

// errorView is the error page's data: the language, and nothing else.
type errorView struct{ Trans trans }

// notFound answers a link that names no team this portal serves.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	t := transOf(r)
	s.render(w, t, http.StatusNotFound, render(template.Must(template.New("notfound").Funcs(t.funcs()).Parse(notFoundHTML)), errorView{Trans: t}))
}

// pageFuncs are what the templates use besides their values, and the
// language they say their words in.
func pageFuncs(t trans) template.FuncMap {
	funcs := t.funcs()
	funcs["windows"] = func(r *Reading, now time.Time) []windowRow { return windows(t, r, now) }
	funcs["ago"] = func(now, at time.Time) string { return ago(t, now, at) }
	funcs["until"] = func(now, at time.Time) string { return until(t, now, at) }
	return funcs
}

func accountsPage(t trans, p Accounts) string {
	p.Trans = t
	return render(template.Must(template.New("accounts").Funcs(pageFuncs(t)).Parse(accountsHTML)), p)
}

// render runs a template into a string. The templates are parsed here,
// from constants: there is no file to be found at run time, so a page
// can't fail to load because a file was left out of an image.
func render(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		// the templates are constants and their data is a struct: a
		// failure here is a bug, and half a page is worse than none
		return "<!doctype html><title>teamd</title><p>this page couldn't be drawn"
	}
	return b.String()
}
