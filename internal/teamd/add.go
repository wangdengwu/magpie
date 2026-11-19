package teamd

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Adding an account is magpie's paste-back sign-in, run from the portal:
// magpie makes the vendor's link, someone opens it in a browser on their
// own computer, and the address that browser ends on — which points at the
// magpie container's loopback, so it loads nothing there — is pasted back
// and handed to magpie. The account magpie signs in is then put in the
// team's group, which is what makes it serve that team.
//
// It is plain forms, with no script on any page: the portal's CSP is
// `default-src 'none'`, and a flow that needs no script needs no exception
// to it. A page's jobs are to show a link, take a paste, and say what
// happened.
//
// Two things here are the portal's own, not magpie's:
//
//   - magpie's sign-ins are global, by an id any caller of the admin API
//     could name. A sign-in started here is remembered against the team that
//     started it, and every later step — its page, its paste, its cancel —
//     is refused for any other team. Without that, one team's lead could
//     read another's vendor link and sign an account in as themselves, or
//     cancel a sign-in out from under them.
//   - the account goes into one group: the team's, read again at the moment
//     it is written, so a group ops changed during the sign-in is the one
//     added to.

// SignIn is a sign-in as the portal's pages show it.
type SignIn struct {
	ID    string
	URL   string
	State string // waiting, done, failed or canceled
	// PasteCallback says the address the browser ends on is pasted back;
	// PasteCode says the code its page shows is.
	PasteCallback bool
	PasteCode     bool
	// User and Plan are the account, once it is signed in.
	User  string
	Plan  string
	Error string
	// Again is an account magpie already had, its sign-in renewed rather
	// than a new account added.
	Again bool
}

// Done says the sign-in finished and magpie has the account.
func (s SignIn) Done() bool { return s.State == "done" }

// Waiting says the sign-in will still take a browser.
func (s SignIn) Waiting() bool { return s.State == "waiting" }

// signInTTL is how long a sign-in the portal started waits to be finished
// before it is cancelled at magpie. A link opened on a laptop and never
// pasted back would otherwise leave magpie's `claude auth login` running.
const signInTTL = 15 * time.Minute

// signIns are the sign-ins this portal started, by the team that started
// them. They are held in memory: a restart loses the links it handed out and
// nothing else, since an account signed in is in magpie already.
type signIns struct {
	mu sync.Mutex
	m  map[string]started
}

type started struct {
	team string
	at   time.Time
}

func newSignIns() *signIns { return &signIns{m: map[string]started{}} }

// start records a sign-in as the team's.
func (s *signIns) start(id, team string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = started{team: team, at: at}
}

// ownedBy says the team started the sign-in. One this portal didn't start
// is nobody's here, whatever magpie says of it — which is what keeps a
// guessed id from reaching another team's browser tab.
func (s *signIns) ownedBy(id, team string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[id]
	return ok && strings.EqualFold(st.team, team)
}

// forget takes a sign-in off the list, done or not.
func (s *signIns) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// stale are the sign-ins started before cutoff, which are cancelled at
// magpie and forgotten: links nobody came back with.
func (s *signIns) stale(cutoff time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for id, st := range s.m {
		if st.at.Before(cutoff) {
			out = append(out, id)
			delete(s.m, id)
		}
	}
	return out
}

// signInState is magpie's answer to one of the sign-in routes, as far as
// the portal reads it.
type signInState struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	State         string `json:"state"`
	PasteCallback bool   `json:"pasteCallback,omitempty"`
	PasteCode     bool   `json:"pasteCode,omitempty"`
	User          string `json:"user,omitempty"`
	Plan          string `json:"plan,omitempty"`
	Error         string `json:"error,omitempty"`
	Again         bool   `json:"again,omitempty"`
}

func (st signInState) signIn() SignIn {
	return SignIn{
		ID: st.ID, URL: st.URL, State: st.State,
		PasteCallback: st.PasteCallback, PasteCode: st.PasteCode,
		User: st.User, Plan: st.Plan, Error: st.Error, Again: st.Again,
	}
}

// signInAgent is the agent the portal signs in. The pool it serves is
// Claude's, and the pages say so.
const signInAgent = "claude"

// ErrNoSignIn is a sign-in this portal didn't start for this team. It is the
// same answer as one that doesn't exist, so a guessed id says nothing.
var ErrNoSignIn = errors.New("this team has no such sign-in")

// StartSignIn begins a sign-in for the team's pool and remembers it against
// that team. No page is opened anywhere: magpie has no browser, and the link
// is for whoever is signing the account in.
func (s *Server) StartSignIn(ctx context.Context, g Grant) (SignIn, error) {
	var st signInState
	// The body is what magpie's admin route reads: the agent. LinkOnly isn't
	// among it — the admin route never opens a page, so the link comes back
	// here for the portal to show.
	if err := s.admin.post(ctx, "signin", map[string]string{"Agent": signInAgent}, &st); err != nil {
		return SignIn{}, err
	}
	if st.ID == "" {
		return SignIn{}, errors.New("magpie answered no sign-in id")
	}
	s.signIns.start(st.ID, g.Team().ID, s.now())
	return st.signIn(), nil
}

// SignInOf is one of the team's sign-ins as magpie has it now.
func (s *Server) SignInOf(ctx context.Context, g Grant, id string) (SignIn, error) {
	if !s.signIns.ownedBy(id, g.Team().ID) {
		return SignIn{}, ErrNoSignIn
	}
	var st signInState
	if err := s.admin.get(ctx, "signin/"+url.PathEscape(id), &st); err != nil {
		return SignIn{}, err
	}
	return st.signIn(), nil
}

// SubmitCallback hands magpie the address the sign-in's browser ended on (or
// the code its page shows) and puts the account it made into the team's
// group. It answers the group entry the account went in as.
//
// magpie answers only once the sign-in is over, so the account is in magpie
// by the time this returns. What is left is finding its id and writing it
// into the group; the id reaches magpie's own lists a moment after the
// sign-in says done, so the look is retried.
func (s *Server) SubmitCallback(ctx context.Context, g Grant, id, raw string) (SignIn, string, error) {
	if !s.signIns.ownedBy(id, g.Team().ID) {
		return SignIn{}, "", ErrNoSignIn
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SignIn{}, "", errors.New("paste the address the browser ended on, or the code the page shows")
	}
	if err := s.admin.post(ctx, "signin/"+url.PathEscape(id)+"/callback", map[string]string{"URL": raw}, nil); err != nil {
		return SignIn{}, "", err
	}
	st, err := s.SignInOf(ctx, g, id)
	if err != nil {
		return SignIn{}, "", err
	}
	if !st.Done() {
		return st, "", nil
	}
	entry, err := s.entryOf(ctx, st.User)
	if err != nil {
		return st, "", err
	}
	if err := s.addToGroup(ctx, g, entry); err != nil {
		return st, entry, err
	}
	s.audit(g, actAdd, entry, st.User, nil)
	s.signIns.forget(id)
	return st, entry, nil
}

// CancelSignIn stops a sign-in the team started, and forgets it.
func (s *Server) CancelSignIn(ctx context.Context, g Grant, id string) error {
	if !s.signIns.ownedBy(id, g.Team().ID) {
		return ErrNoSignIn
	}
	err := s.admin.post(ctx, "signin/"+url.PathEscape(id)+"/cancel", struct{}{}, nil)
	s.signIns.forget(id)
	if err == nil {
		// the sign-in's id is not recorded: it is a handle on a vendor
		// session, and the line only needs to say the team stopped one
		s.audit(g, actCancel, "", "", nil)
	}
	return err
}

// entryOf is the group entry "<provider>/<stable id>" of the account the
// user is signed in as, read from magpie's account list — the account was
// added a moment ago, so the list may not carry it yet.
func (s *Server) entryOf(ctx context.Context, user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", errors.New("magpie's sign-in finished without saying which account it signed in")
	}
	var last error
	for attempt := range 10 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		catalog, err := s.admin.Catalog(ctx)
		if err != nil {
			last = err
			continue
		}
		for _, a := range catalog.Accounts {
			if !a.Key && strings.EqualFold(strings.TrimSpace(a.User), user) {
				return a.ID, nil
			}
		}
	}
	if last != nil {
		return "", last
	}
	return "", errors.New("magpie signed " + user + " in, but lists no account of it yet: check again in a moment")
}

// addToGroup writes the account into the team's group, as magpie has the
// group now.
//
// SaveAccountGroup replaces the whole list, so this is a read-modify-write:
// it is serialised in this process, and it reads the group again rather than
// trusting the grant's reading — a sign-in takes minutes, and ops may have
// changed the group in them.
func (s *Server) addToGroup(ctx context.Context, g Grant, entry string) error {
	entry = strings.ToLower(strings.TrimSpace(entry))
	if _, ok := cleanAccount(entry); !ok {
		return errors.New("magpie signed the account in, but this portal can't name it in a group")
	}
	s.groupMu.Lock()
	defer s.groupMu.Unlock()
	groups, err := s.admin.AccountGroups(ctx)
	if err != nil {
		return err
	}
	group, ok := findGroup(groups, g.Team().GroupID)
	if !ok {
		return ErrNoGroup
	}
	if slicesContainsFold(group.Accounts, entry) {
		return nil // signed in again: the group holds it now
	}
	group.Accounts = append(group.Accounts, entry)
	var out struct {
		Groups []Group `json:"groups"`
	}
	if err := s.admin.post(ctx, "account-groups/save", group, &out); err != nil {
		return errors.New("magpie signed the account in, but the team's group didn't take it: " + oneLine(err.Error()))
	}
	return nil
}

// signInJanitor cancels the sign-ins nobody came back with, every minute,
// until ctx is done.
func (s *Server) signInJanitor(ctx context.Context) { s.sweepSignIns(ctx, time.Minute) }

// sweepSignIns is the janitor at a period a test can set.
func (s *Server) sweepSignIns(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		for _, id := range s.signIns.stale(s.now().Add(-signInTTL)) {
			// magpie's answer isn't waited on: the sign-in is gone from this
			// portal either way, and a magpie that can't be reached drops it
			// when it restarts
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = s.admin.post(c, "signin/"+url.PathEscape(id)+"/cancel", struct{}{}, nil)
			cancel()
		}
	}
}

// addRoutes are the add flow's routes. Each starts at the boundary: the
// token in the path resolves to a team, and the sign-in named must be one
// that team started.
func (s *Server) addRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /team/{token}/add", s.serveAddPage)
	mux.HandleFunc("POST /team/{token}/add", s.serveAddStart)
	mux.HandleFunc("GET /team/{token}/signin/{id}", s.serveSignInPage)
	mux.HandleFunc("POST /team/{token}/signin/{id}/callback", s.serveSignInCallback)
	mux.HandleFunc("POST /team/{token}/signin/{id}/cancel", s.serveSignInCancel)
}

// addNotice is what a page says about the last thing that happened, and
// whether it went well.
type addNotice struct {
	Text string
	Bad  bool
}

// serveAddPage is where "Add an account" goes: what it will ask for, and the
// button that starts it.
func (s *Server) serveAddPage(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	s.render(w, http.StatusOK, addPage(g, r.PathValue("token"), addNotice{}))
}

// serveAddStart starts a sign-in and sends the browser to its page.
func (s *Server) serveAddStart(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "this form is submitted from the team's own page", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	st, err := s.StartSignIn(ctx, g)
	if err != nil {
		// the sign-in's own page has nothing to show yet: the pool's page it
		// came from says what went wrong
		s.render(w, http.StatusBadGateway, addPage(g, r.PathValue("token"), addNotice{Text: "magpie couldn't start the sign-in: " + err.Error(), Bad: true}))
		return
	}
	http.Redirect(w, r, "/team/"+r.PathValue("token")+"/signin/"+url.PathEscape(st.ID), http.StatusSeeOther)
}

// serveSignInPage shows one sign-in: the link to open, the field to paste
// what it ended on, and the way out.
func (s *Server) serveSignInPage(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	st, err := s.SignInOf(ctx, g, r.PathValue("id"))
	if errors.Is(err, ErrNoSignIn) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.pageError(w, err)
		return
	}
	s.render(w, http.StatusOK, signInPage(g, r.PathValue("token"), st, addNotice{}))
}

// serveSignInCallback takes the pasted address, and sends the browser back to
// the accounts page when the account is in the group.
func (s *Server) serveSignInCallback(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "this form is submitted from the team's own page", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form didn't read", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	st, entry, err := s.SubmitCallback(ctx, g, r.PathValue("id"), r.PostFormValue("url"))
	if errors.Is(err, ErrNoSignIn) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		// magpie's own words for a bad paste are the right ones: they say
		// which address it wanted. The paste itself is never echoed back.
		s.render(w, http.StatusBadRequest, signInPage(g, r.PathValue("token"), st, addNotice{Text: err.Error(), Bad: true}))
		return
	}
	if !st.Done() {
		s.render(w, http.StatusOK, signInPage(g, r.PathValue("token"), st, addNotice{Text: "magpie hasn't finished with that yet: check again in a moment.", Bad: true}))
		return
	}
	http.Redirect(w, r, "/team/"+r.PathValue("token")+"?added="+url.QueryEscape(entry), http.StatusSeeOther)
}

// serveSignInCancel stops a sign-in and goes back to the pool.
func (s *Server) serveSignInCancel(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "this form is submitted from the team's own page", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := s.CancelSignIn(ctx, g, r.PathValue("id"))
	if errors.Is(err, ErrNoSignIn) {
		// the same answer a sign-in that doesn't exist gets: a team learns
		// nothing of another's, and a cancel that changed nothing says so
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.pageError(w, err)
		return
	}
	http.Redirect(w, r, "/team/"+r.PathValue("token"), http.StatusSeeOther)
}

// sameOrigin says a form's post came from a page of this portal.
//
// Origin alone can't answer it. Every page here is sent with
// Referrer-Policy: no-referrer — the token is in the address, and a Referer
// would hand it to whatever the page reached — and Chrome and Safari
// withhold Origin as well under that policy: a form posted from this
// portal's own page in Chromium arrives with `Origin: null`, which reads
// exactly like a sandboxed frame's post. Judging Origin alone refused every
// switch and every sign-in from a real browser.
//
// So Sec-Fetch-Site decides when it is there. The browser sets it and no
// page can: `same-origin` is this portal's own page, `cross-site` and
// `same-site` are not, and `none` is a navigation with no page behind it.
// It is a header a script can write, but a script already holds the token
// from the address, so nothing is kept from it that wasn't given.
//
// Without it — curl, an older browser, a test — Origin decides: absent is
// no page at all and is let through, `null` is an opaque origin, which is
// never a page of this portal, and any other must be this host.
func sameOrigin(r *http.Request) bool {
	if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" {
		return strings.EqualFold(site, "same-origin")
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	switch {
	case origin == "":
		return true
	case strings.EqualFold(origin, "null"):
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
