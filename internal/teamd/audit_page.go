package teamd

import (
	"net/http"
	"time"
)

// The audit page: what this team's lead did through the portal, and what
// the portal refused.
//
// It reads teamd's own file, filtered by the grant's team id — never
// another team's lines, and never everything. A lead's page shows the last
// AuditPage of them; ops reads the file itself, which is the one place the
// whole record is.

// AuditRow is one line as the page draws it.
type AuditRow struct {
	At     time.Time
	Action string
	// Verb is the action in the page's words: "added an account".
	Verb string
	// Account is the entry, and Name the account's shown name when the
	// line has one.
	Account string
	Name    string
	// Who made the change, in the page's words: the team's lead, or ops.
	Who string
	// Refused says the portal turned this request away.
	Refused bool
}

// AuditView is the page's data.
type AuditView struct {
	Trans trans
	Team  Team
	Token string
	// Rows are the team's lines, oldest first, and Total how many there
	// are.
	Rows  []AuditRow
	Total int
	Now   time.Time
}

// verbOf is an audit action in the page's words. An action this build
// doesn't know is said as it is: a line from a newer teamd is still a line,
// in either language.
func verbOf(t trans, action string) string {
	switch action {
	case actAdd:
		return t.T("audit.verb.add")
	case actOff:
		return t.T("audit.verb.off")
	case actOn:
		return t.T("audit.verb.on")
	case actCancel:
		return t.T("audit.verb.cancel")
	case actTeamAdd:
		return t.T("audit.verb.team-add")
	case actTeamToken:
		return t.T("audit.verb.team-token")
	case actTeamRemove:
		return t.T("audit.verb.team-rm")
	case actKeyIssue:
		return t.T("audit.verb.key-issue")
	case actKeyRevoke:
		return t.T("audit.verb.key-revoke")
	}
	return action
}

// whoOf is who an entry names, in the page's words.
func whoOf(t trans, e Entry) string {
	if e.Who == OpsWho {
		return t.T("audit.who.ops")
	}
	if e.Who == e.Team || e.Who == "" {
		return t.T("audit.who.link")
	}
	return e.Who
}

// AuditViewFor is a team's own lines as the page shows them, through its
// grant: a page can not ask for another team's, and there is no route that
// takes a team id.
func (s *Server) AuditViewFor(g Grant, t trans) (AuditView, error) {
	lines, err := s.LinesFor(g)
	if err != nil {
		return AuditView{}, err
	}
	page := AuditView{Trans: t, Team: g.Team(), Total: lines.Total, Now: s.now()}
	for _, e := range lines.Entries {
		page.Rows = append(page.Rows, AuditRow{
			At:      e.At,
			Action:  e.Action,
			Verb:    verbOf(t, e.Action),
			Account: e.Account,
			Name:    e.Name,
			Who:     whoOf(t, e),
			Refused: e.Result == ResultDenied,
		})
	}
	return page, nil
}

// auditRoutes is the audit page's route.
func (s *Server) auditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /team/{token}/audit", s.serveAudit)
}

// serveAudit answers GET /team/{token}/audit.
func (s *Server) serveAudit(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	page, err := s.AuditViewFor(g, transOf(r))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	s.render(w, transOf(r), http.StatusOK, auditPage(page))
}
