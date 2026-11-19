package teamd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// The members page: the gateway keys a team's lead has issued to their
// members, and the form that issues one.
//
// The secret is shown on the answer to the form and nowhere else: the
// list is drawn from magpie's own list, which carries no secret, so a
// reload of the page cannot show one. The issuer is the team's own
// gateway key, taken from the team record — a request that names an
// issuer has it ignored.

// memberTTLs are the lifetimes the form offers, as the values it posts and
// the words it shows. "forever" is the cap the constants hold: a hundred
// years, which the page draws as permanent.
// memberTTL is one lifetime the form offers: the value it posts, the word
// key it is shown by, and the lifetime itself.
type memberTTL struct {
	Value string
	Key   string
	TTL   time.Duration
}

var memberTTLs = []memberTTL{
	{"7d", "members.ttl.7d", 7 * 24 * time.Hour},
	{"30d", "members.ttl.30d", 30 * 24 * time.Hour},
	{"90d", "members.ttl.90d", 90 * 24 * time.Hour},
	{"forever", "members.ttl.forever", MaxMemberTTL},
}

// MaxMemberTTL is how long "permanent" is: magpie's cap for an issued key,
// which the page draws as permanent rather than as a date a century out.
const MaxMemberTTL = 100 * 365 * 24 * time.Hour

// ttlOf is the lifetime a posted value names, and whether it is one.
func ttlOf(v string) (time.Duration, bool) {
	for _, o := range memberTTLs {
		if o.Value == v {
			return o.TTL, true
		}
	}
	return 0, false
}

// foreverAs is the point past which a key's expiry is drawn as permanent.
// A year short of the cap, so a key issued a moment ago reads so too.
const foreverAs = 50 * 365 * 24 * time.Hour

// Members is the page's data.
type Members struct {
	Team  Team
	Token string
	// Issuer is the team's gateway key, which every key here is issued
	// under.
	Issuer string
	// Rows are the team's member keys, newest first.
	Rows []MemberRow
	// CanIssue is whether the team's key may issue, and Why says why not
	// when it may not: the page offers the form only then.
	CanIssue bool
	Why      string
	// Issued is the key the form just made, with its secret: the only
	// draw of this page that carries one.
	Issued *MemberKey
	// IssuedUntil is when Issued stops working, in the page's words.
	IssuedUntil string
	// TTLs are the lifetimes the form offers.
	TTLs []memberTTL
	// Trans is the language the page is drawn in.
	Trans trans
	// Notice is what the last action did, "" when this draw isn't the
	// answer to one.
	Notice switchNotice
	// Now is the clock the page's dates are counted from.
	Now time.Time
}

// MemberRow is one member key as the page draws it.
type MemberRow struct {
	ID   string
	Name string
	// Expires is when it stops working; Permanent says it is drawn as
	// permanent rather than as that date.
	Expires   time.Time
	Permanent bool
	// Expired is whether it no longer works.
	Expired bool
	// When is Expires in the page's words, for a key that isn't permanent.
	When string
}

// membersFor reads the page's data.
func (s *Server) membersFor(ctx context.Context, t trans, g Grant) (Members, error) {
	page := Members{Trans: t, Team: g.Team(), Issuer: g.Team().KeyID, Now: s.now()}
	keys, err := s.admin.GatewayKeys(ctx)
	if err != nil {
		return Members{}, err
	}
	for _, k := range keys {
		if k.ID != page.Issuer {
			continue
		}
		switch {
		case k.Off:
			page.Why = t.T("members.why.off")
		case !k.Issues:
			page.Why = t.T("members.why.not_issuer", argsOf{"ID": k.ID})
		default:
			page.CanIssue = true
		}
	}
	if !page.CanIssue && page.Why == "" {
		page.Why = t.T("members.why.no_key")
	}
	issued, err := s.admin.MemberKeys(ctx, page.Issuer)
	if err != nil {
		return Members{}, err
	}
	now := s.now()
	for _, k := range issued {
		row := MemberRow{ID: k.ID, Name: k.Name, Expires: k.Expires}
		row.Permanent = k.Expires.Sub(now) >= foreverAs
		row.Expired = !now.Before(k.Expires)
		if !row.Permanent {
			row.When = k.Expires.Local().Format("2006-01-02 15:04")
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// membersRoutes are the members page's routes.
func (s *Server) membersRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /team/{token}/members", s.serveMembers)
	mux.HandleFunc("POST /team/{token}/members", s.serveIssueMember)
	mux.HandleFunc("POST /team/{token}/members/{id}/revoke", s.serveRevokeMember)
}

// serveMembers answers GET /team/{token}/members.
func (s *Server) serveMembers(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	page, err := s.membersFor(ctx, transOf(r), g)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	page.TTLs = memberTTLs
	s.render(w, transOf(r), http.StatusOK, membersPage(page))
}

// serveIssueMember answers the form: issue a key under the team's own key,
// and draw the page with the one answer that carries the secret.
func (s *Server) serveIssueMember(w http.ResponseWriter, r *http.Request) {
	t := transOf(r)
	if !sameOrigin(r) {
		http.Error(w, t.T("signin.err.origin"), http.StatusForbidden)
		return
	}
	g, ok := s.issueGrant(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := r.ParseForm(); err != nil {
		http.Error(w, t.T("signin.err.form"), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		s.renderMembers(w, r, g, http.StatusBadRequest, switchNotice{Text: t.T("members.notice.no_name"), Bad: true})
		return
	}
	ttl, ok := ttlOf(r.PostFormValue("ttl"))
	if !ok {
		s.renderMembers(w, r, g, http.StatusBadRequest, switchNotice{Text: t.T("members.notice.no_ttl"), Bad: true})
		return
	}
	// the issuer is the team's own, never a request's: a field naming
	// another key is ignored. The key is asked first, so a team whose key
	// may not issue gets this portal's words rather than magpie's.
	if page, err := s.membersFor(ctx, t, g); err != nil || !page.CanIssue {
		why := t.T("members.notice.cant")
		if err == nil && page.Why != "" {
			why = page.Why
		}
		s.renderMembers(w, r, g, http.StatusBadRequest, switchNotice{Text: why, Bad: true})
		return
	}
	k, err := s.admin.IssueMemberKey(ctx, g.Team().KeyID, name, ttl)
	s.audit(g, actKeyIssue, "", name, err)
	if err != nil {
		s.renderMembers(w, r, g, http.StatusBadRequest, switchNotice{Text: s.admin.scrub(err.Error()), Bad: true})
		return
	}
	s.renderMembers(w, r, g, http.StatusOK, switchNotice{}, &k)
}

// serveRevokeMember answers the list's revoke: a key issued under this
// team's own key, and nothing else.
func (s *Server) serveRevokeMember(w http.ResponseWriter, r *http.Request) {
	t := transOf(r)
	if !sameOrigin(r) {
		http.Error(w, t.T("signin.err.origin"), http.StatusForbidden)
		return
	}
	g, ok := s.issueGrant(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id := strings.TrimSpace(r.PathValue("id"))
	err := s.admin.RevokeMemberKey(ctx, g.Team().KeyID, id)
	s.audit(g, actKeyRevoke, "", id, err)
	if err != nil {
		s.renderMembers(w, r, g, http.StatusNotFound, switchNotice{Text: t.T("members.notice.gone"), Bad: true})
		return
	}
	s.renderMembers(w, r, g, http.StatusOK, switchNotice{Text: t.T("members.notice.revoked", argsOf{"ID": id})})
}

// renderMembers draws the page with a notice, reading its data again so
// the list is magpie's after the action, not the page's guess.
func (s *Server) renderMembers(w http.ResponseWriter, r *http.Request, g Grant, status int, notice switchNotice, issued ...*MemberKey) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	page, err := s.membersFor(ctx, transOf(r), g)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	page.Notice = notice
	page.TTLs = memberTTLs
	if len(issued) > 0 {
		page.Issued = issued[0]
		t := transOf(r)
		page.IssuedUntil = t.T("members.issued.until", argsOf{"At": issued[0].Expires.Local().Format("2006-01-02 15:04")})
		if issued[0].Expires.Sub(page.Now) >= foreverAs {
			page.IssuedUntil = t.T("members.issued.forever")
		}
	}
	s.render(w, transOf(r), status, membersPage(page))
}

// issueGrant is the boundary for the members page's posts: the team's own
// token, naming no account.
func (s *Server) issueGrant(w http.ResponseWriter, r *http.Request) (Grant, bool) {
	g, err := s.Authorize(r.Context(), r.PathValue("token"), Target{})
	switch {
	case err == nil:
		return g, true
	case errors.Is(err, ErrNoTeam):
		s.notFound(w, r)
	case errors.Is(err, ErrNotYours):
		http.Error(w, transOf(r).T("signin.err.not_yours"), http.StatusForbidden)
	default:
		s.pageError(w, r, err)
	}
	return Grant{}, false
}
