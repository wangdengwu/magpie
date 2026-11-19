package teamd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Disabling an account takes it out of the team's pool: magpie keeps it
// signed in and serves nothing from it until it is switched back on.
//
// It is the one change a team's lead makes to an account that already
// exists, and the only one. Adding is the sign-in flow (add.go). Removing
// is ops's, through magpie's own app or command line: there is no route
// here that takes an account out of a group, and none that forgets a
// sign-in, so nothing a lead can reach removes an account.
//
// What may be switched is the boundary's answer, not the page's: the entry
// in the form resolves to an account of this team's group, read from magpie
// for this request. An account of another team's, an account ops took out
// of this group since the page was drawn, and an entry that names no
// account at all are refused before magpie is told anything.
//
// The entry travels in the form body rather than the path. A group entry is
// "<provider>/<stable id>", and Go's router matches a single path segment
// per wildcard, so a path like /accounts/claude/<id>/off would have to
// spell the one name two ways. Both are equally the caller's to type, and
// both go through the same check.

// ErrNoAccount is the account an action named, as this team may not act on
// it: it is not in this team's group, or magpie has no such account. It is
// one answer on purpose. "Not in your group" and "no such account" are the
// same fact about a link's reach, and telling them apart would say which
// ids magpie has.
var ErrNoAccount = errors.New("this team has no such account")

// notSwitchable is an account this team does hold, and that magpie has no
// sign-in of to switch: a key, or an entry magpie lists under no sign-in.
// Its message is the page's own words, since nothing in it is a secret.
type notSwitchable struct{ why string }

func (e notSwitchable) Error() string { return e.why }

// NotSwitchable says err is an account with no sign-in to switch, and
// answers the page's words for it.
func NotSwitchable(err error) (string, bool) {
	var e notSwitchable
	if errors.As(err, &e) {
		return e.why, true
	}
	return "", false
}

// errLostEntry is an entry the team's group holds that names no account
// magpie has: signed out since, or an id magpie no longer keeps. It is the
// team's own entry, so saying so tells the lead nothing of another team.
func errLostEntry(t trans) notSwitchable { return notSwitchable{t.T("switch.lost_entry")} }

// Switch changes whether magpie serves from one of the team's accounts.
//
// What may be switched is the boundary's grant, never the request: the
// account reached here has already been checked against this team's group,
// read from magpie for this request, so an account of another team's, one
// ops took out of this one since the page was drawn, and a name no group
// holds are all refused before magpie is told anything (switchGrant).
// Switch checks the grant again itself, so it is a promise of the write and
// not only of the route that calls it.
//
// What is left is magpie's own account list: an entry the group holds that
// names no account, and a key with no sign-in, have nothing to switch.
// What the switch did is read back afterwards (accountNow), not assumed.
func (s *Server) Switch(ctx context.Context, t trans, g Grant, entry string, on bool) error {
	err := s.switchAccount(ctx, t, g, entry, on)
	// the name is read only when the switch went through: a refusal's line
	// is worth writing whether or not magpie answers, and the name is
	// magpie's
	name := ""
	if err == nil {
		name = s.auditName(ctx, g, entry)
	}
	s.audit(g, switchAction(on), entry, name, err)
	return err
}

// switchAction is the audit line's word for a switch.
func switchAction(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// switchAccount is Switch without the audit line, so the line is written
// whatever the outcome — including a refusal, which is the line a lead
// most needs when a key was used by something that shouldn't have it.
func (s *Server) switchAccount(ctx context.Context, t trans, g Grant, entry string, on bool) error {
	entry = strings.ToLower(strings.TrimSpace(entry))
	if want, ok := cleanAccount(entry); !ok || want != entry {
		return ErrNoAccount
	}
	// The same check the boundary made, said again where the write is: it
	// is this function's own promise that a grant only ever changes an
	// account of its group, and a second caller of it would otherwise have
	// only the boundary's word for that.
	if !g.Holds(entry) {
		return ErrNoAccount
	}
	catalog, err := s.admin.Catalog(ctx)
	if err != nil {
		return err
	}
	acct, ok := catalog.Accounts[entry]
	if !ok {
		// The group holds it, and magpie has nothing by that id. The lead's
		// own page says so, so a post that asks anyway is answered rather
		// than refused as another team's: the entry is this team's, and it
		// says nothing about any other.
		return errLostEntry(t)
	}
	if acct.Key {
		return notSwitchable{t.T("switch.why.keylist")}
	}
	if !acct.SignedIn || acct.Agent == "" || acct.User == "" {
		return notSwitchable{t.T("switch.why.no_signin_long")}
	}
	return s.loginOn(ctx, acct.Agent, acct.User, on)
}

// loginOn tells magpie to serve from an account or to leave it alone. Its
// route is the app's own switch, on the agent and the user signed in —
// never on the id a group keeps the account by.
func (s *Server) loginOn(ctx context.Context, agent, user string, on bool) error {
	action := "off"
	if on {
		action = "on"
	}
	return s.admin.post(ctx, "login/"+action, map[string]string{"Agent": agent, "User": user}, nil)
}

// accountNow is one account as magpie has it after a change. A magpie that
// can't be read is an error, not a guess: the change may well have landed,
// and a page that said "disabled" over a read that failed would be making
// it up.
func (s *Server) accountNow(ctx context.Context, entry string) (Account, error) {
	catalog, err := s.admin.Catalog(ctx)
	if err != nil {
		return Account{}, err
	}
	return catalog.Accounts[entry], nil
}

// switchRoutes are the two controls' routes.
func (s *Server) switchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /team/{token}/accounts/off", s.serveSwitch(false))
	mux.HandleFunc("POST /team/{token}/accounts/on", s.serveSwitch(true))
}

// switchNotice is what the pool's page says about the last switch.
type switchNotice struct {
	Text string
	Bad  bool
}

// serveSwitch answers one of the two controls: change the account, then
// draw the pool again from magpie.
func (s *Server) serveSwitch(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := transOf(r)
		if !sameOrigin(r) {
			http.Error(w, t.T("signin.err.origin"), http.StatusForbidden)
			return
		}
		g, ok := s.switchGrant(w, r, on)
		if !ok {
			return
		}
		entry := strings.ToLower(strings.TrimSpace(r.PostFormValue("account")))
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		err := s.Switch(ctx, t, g, entry, on)
		if err != nil {
			// magpie's own refusal where it gave one, and this portal's
			// words where the objection is its own. Nothing magpie says
			// reaches a page unfiltered: its errors can name another team's
			// accounts (switchFailed, pageError).
			s.doneWith(w, r, switchFailed(t, err, on))
			return
		}
		// magpie took it. What the page says of it is read from magpie, not
		// assumed from the request: magpie may have taken the change and
		// still serve nothing from it.
		acct, err := s.accountNow(ctx, entry)
		if err != nil {
			s.doneWith(w, r, t.T("switch.done.unreadable"))
			return
		}
		name := acct.Name
		if name == "" {
			name = entry
		}
		why := argsOf{"Name": name, "Why": whyNotServing(t, acct)}
		switch {
		case on && acct.On:
			s.doneWith(w, r, t.T("switch.done.on", argsOf{"Name": name}))
		case on:
			s.doneWith(w, r, t.T("switch.done.not_serving", why))
		case acct.Off:
			s.doneWith(w, r, t.T("switch.done.off", argsOf{"Name": name}))
		default:
			s.doneWith(w, r, t.T("switch.done.still", why))
		}
	}
}

// whyNotServing says why an account magpie has is not serving, in the
// page's words.
func whyNotServing(t trans, acct Account) string {
	switch {
	case acct.Paused:
		return t.T("switch.why.not_serving.paused")
	case acct.On:
		return t.T("switch.why.not_serving.serving")
	default:
		return t.T("switch.why.not_serving")
	}
}

// switchFailed is what the page says when the change didn't happen, in
// magpie's own words where it refused: "… is the only claude account in
// use; turn another on first to pause it" is a thing a lead can act on, and
// "the management API answered 400" is not.
//
// A failure that isn't magpie refusing the change says only that. Its
// words are not repeated: an error from a read can name another team's
// accounts, and a page is not where those go.
func switchFailed(t trans, err error, on bool) string {
	if why, ok := NotSwitchable(err); ok {
		return why
	}
	var said *MagpieError
	if errors.As(err, &said) && said.Message != "" {
		// magpie's own words, which say what a lead can act on. The
		// sentence around them is the portal's, and is translated.
		if on {
			return t.T("switch.failed.on", argsOf{"Why": said.Message})
		}
		return t.T("switch.failed.off", argsOf{"Why": said.Message})
	}
	return t.T("switch.failed.unreachable")
}

// switchGrant is the boundary for a switch's post. The account is the
// target, so a link naming another team's account is refused here, before
// magpie is asked for anything but the group the token resolves to.
func (s *Server) switchGrant(w http.ResponseWriter, r *http.Request, on bool) (Grant, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, transOf(r).T("signin.err.form"), http.StatusBadRequest)
		return Grant{}, false
	}
	entry := strings.ToLower(strings.TrimSpace(r.PostFormValue("account")))
	// A switch always names an account, so a form without one is refused
	// here: an empty Target.Account is how the boundary is asked for no
	// account at all (a page that only lists the team), and a blank field
	// is not that.
	if want, ok := cleanAccount(entry); !ok || want != entry {
		http.Error(w, transOf(r).T("signin.err.not_yours"), http.StatusForbidden)
		return Grant{}, false
	}
	g, err := s.Authorize(r.Context(), r.PathValue("token"), Target{Account: entry})
	switch {
	case err == nil:
		return g, true
	case errors.Is(err, ErrNoTeam), errors.Is(err, ErrNoGroup):
		s.notFound(w, r)
	case errors.Is(err, ErrNotYours), errors.Is(err, ErrNoAccount):
		// an account of another team's, one ops took out of this group since
		// the page was drawn, and a name no group could hold all get the
		// same answer, before magpie is told to change anything.
		//
		// It is recorded: a line saying a team's own link asked for an
		// account that isn't its own is the record a lead needs when a key
		// has got out.
		s.auditRefused(r.PathValue("token"), switchAction(on), entry, err)
		http.Error(w, transOf(r).T("signin.err.not_yours"), http.StatusForbidden)
	default:
		s.pageError(w, r, err)
	}
	return Grant{}, false
}

// doneWith draws the pool's page again with a notice, so a switch's answer
// is the page the lead was looking at, read from magpie as it is now.
//
// It is written from the post rather than redirected to, so the browser
// isn't asked for the page a second time; the pool's own address still
// reads the same on a reload, since the post changed an account and not the
// page.
func (s *Server) doneWith(w http.ResponseWriter, r *http.Request, text string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	g, err := s.Authorize(ctx, r.PathValue("token"), Target{})
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page, err := s.AccountsFor(ctx, g, transOf(r))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	page.Notice = switchNotice{Text: text}
	s.render(w, transOf(r), http.StatusOK, accountsPage(transOf(r), page))
}

// switchForm is what the pool's page needs to draw one account's control:
// the route's last part, and the word on the button.
type switchForm struct {
	// Action is "off" or "on", the route's last part.
	Action string
	// Label is the button's words.
	Label string
}

// switchable says the row has a sign-in for a switch to act on.
func (row AccountRow) switchable() bool {
	return row.Known && !row.Key && row.SignedIn
}

// WhyNot is why an account's row carries no control, in the page's words,
// "" when it does or when the row's own note has said it. An account magpie
// has no sign-in of can't be switched, and neither can a key: there is
// nothing for the switch to act on.
func (row AccountRow) WhyNot(t trans) string {
	switch {
	case !row.Known:
		// the row's note already says magpie has no account by this id, and
		// saying it twice reads as two faults
		return ""
	case row.Key:
		return t.T("switch.why.key")
	case !row.SignedIn:
		return t.T("switch.why.no_signin")
	}
	return ""
}

// Switch is the control one account's row carries: Disable for an account
// in use, Enable for one that isn't. An account that can't be switched has
// none — whether or not WhyNot has something to say about it.
func (row AccountRow) Switch(t trans) *switchForm {
	if !row.switchable() {
		return nil
	}
	if row.On {
		return &switchForm{Action: "off", Label: t.T("switch.off")}
	}
	return &switchForm{Action: "on", Label: t.T("switch.on")}
}

// Warning is what the page says beside the control before it is pressed, ""
// when there is nothing to warn of.
//
// It is on the page and not a step of its own: the portal runs no script,
// so a confirm dialog would need one, and a pool of one is a pool a lead may
// well mean to empty — the warning says what happens, and does not stand in
// the way.
func (row AccountRow) Warning(t trans) string {
	if !row.On || row.Others > 0 {
		return ""
	}
	return t.T("switch.warn.last")
}
