package teamd

import (
	"context"
	"log"
	"strings"
)

// What the portal records: one line per change it made, and one per change
// it refused.
//
// A refused line is the point of the file as much as an allowed one. A
// gateway key that something else has got hold of reaches magpie with this
// portal's admin key only through these routes, so a line saying the team's
// own link tried an account that isn't its own is the record a lead needs.
//
// What is never in a line: a team token, the admin key, the team's gateway
// key, and anything a browser pasted (a sign-in's callback carries a code).
// The account is the group entry magpie keeps it by, and the name it was
// shown by, which are both magpie's own and neither is a secret.

// The actions the portal records, as an audit line says them.
const (
	actAdd        = "add"
	actOff        = "off"
	actOn         = "on"
	actCancel     = "signin-cancel"
	actTeamAdd    = "team-add"
	actTeamToken  = "team-token"
	actTeamRemove = "team-rm"
	actKeyIssue   = "key-issue"
	actKeyRevoke  = "key-revoke"
)

// audit writes a line for what an action did, and never fails one: a
// portal that can't write its record must still work, and the failure goes
// where ops looks rather than to the member's page.
//
// The account's shown name is looked up only when there is an account and
// the action didn't fail: it needs magpie, and a refusal's line is worth
// writing whether or not magpie answers.
func (s *Server) audit(g Grant, action, account, name string, err error) {
	e := Entry{
		At:      s.now().UTC(),
		Team:    g.Team().ID,
		Action:  action,
		Account: strings.ToLower(strings.TrimSpace(account)),
		Name:    strings.TrimSpace(name),
		Who:     g.Team().ID,
		Result:  ResultOK,
	}
	if err != nil {
		e.Result = ResultDenied
	}
	if werr := s.store.Audit(e); werr != nil {
		log.Printf("teamd: an audit line for %s %s wasn't written: %v", e.Team, e.Action, werr)
	}
}

// auditName is the name an account is shown by, for the line's Name: "" if
// magpie can't be asked, which is not a reason to fail the action.
func (s *Server) auditName(ctx context.Context, g Grant, entry string) string {
	catalog, err := s.admin.Catalog(ctx)
	if err != nil {
		return ""
	}
	if acct, ok := catalog.Accounts[strings.ToLower(strings.TrimSpace(entry))]; ok {
		return acct.Name
	}
	return ""
}

// auditRefused records a request the boundary turned away — the line a
// lead needs when a link or a key was used for something that isn't that
// team's.
//
// It takes the token rather than a grant because a refusal is what there is
// instead of one. One thing is therefore checked twice: the token is
// resolved again, and a token that names no team writes nothing. There is
// no team to write such a line under, and a record of guesses is not this
// file's job. The route's own check already refused it a line earlier, so
// this is the promise said where the write is, not a second reading of it.
func (s *Server) auditRefused(token, action, account string, refusal error) {
	if refusal == nil {
		return
	}
	team, err := s.teamOf(token)
	if err != nil {
		return
	}
	e := Entry{
		At:      s.now().UTC(),
		Team:    team.ID,
		Action:  action,
		Account: strings.ToLower(strings.TrimSpace(account)),
		Who:     team.ID,
		Result:  ResultDenied,
	}
	if werr := s.store.Audit(e); werr != nil {
		log.Printf("teamd: a refused %s for %s wasn't written: %v", action, team.ID, werr)
	}
}

// AuditFor is what a team's own page reads: the last of its lines and how
// many there are.
type AuditFor struct {
	Entries []Entry
	// Total is how many of the team's lines there are, beyond what is
	// shown.
	Total int
}

// AuditPage is how many lines a page shows. A team's history is not a log
// viewer: the last few are what a lead checks, and the file is ops's.
const AuditPage = 100

// LinesFor reads a team's own audit lines through its grant, so a page
// can't ask for another's.
func (s *Server) LinesFor(g Grant) (AuditFor, error) {
	entries, total, err := s.store.AuditLines(g.Team().ID, AuditPage)
	if err != nil {
		return AuditFor{}, err
	}
	return AuditFor{Entries: entries, Total: total}, nil
}
