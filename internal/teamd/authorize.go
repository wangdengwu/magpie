package teamd

import (
	"context"
	"fmt"
	"strings"
)

// Authorize is the portal's whole security boundary: the only way an
// action learns which team it is for.
//
// It turns token into the one team it names, reads that team's account
// group from magpie as magpie has it now, and answers a Grant only if the
// target it was given belongs to that group. Everything an action names
// must be in target: an account, a group, or nothing for the pages that
// only list the team. What it does not name is not checked, so a caller
// adds to target rather than around it.
//
// magpie's management API is global, so a grant is the only thing its
// writes take. A refusal is ErrNoTeam (404, and nothing about why) or
// ErrNotYours (403): a link that a chat client cut short learns nothing
// about which part was wrong, and a team learns nothing about another.
func (s *Server) Authorize(ctx context.Context, token string, target Target) (Grant, error) {
	team, err := s.teamOf(token)
	if err != nil {
		return Grant{}, err
	}
	// The group is read on every action, not kept: a group ops changed, or
	// removed, holds from that moment. A read that fails refuses the
	// action — a boundary that can't see the group can't say what it holds.
	groups, err := s.admin.AccountGroups(ctx)
	if err != nil {
		return Grant{}, err
	}
	group, ok := findGroup(groups, team.GroupID)
	if !ok {
		return Grant{}, fmt.Errorf("team %s names the account group %q, which magpie has not: %w", team.ID, team.GroupID, ErrNoGroup)
	}
	if g := strings.TrimSpace(target.Group); g != "" && !strings.EqualFold(g, group.ID) {
		return Grant{}, fmt.Errorf("the account group %q is not this team's: %w", g, ErrNotYours)
	}
	if a := strings.TrimSpace(target.Account); a != "" {
		want, shape := cleanAccount(a)
		if !shape || !slicesContainsFold(group.Accounts, want) {
			return Grant{}, fmt.Errorf("the account %q is not in this team's group: %w", a, ErrNotYours)
		}
	}
	return Grant{team: team, group: group}, nil
}

// teamOf is the team token names. Every team's hash is compared, so which
// one a token was issued for isn't a thing the time it took tells.
func (s *Server) teamOf(token string) (Team, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Team{}, ErrNoTeam
	}
	teams, err := s.store.Teams()
	if err != nil {
		return Team{}, err
	}
	sum := HashToken(token)
	var found Team
	matched := false
	for _, t := range teams {
		if t.TokenHash == "" {
			continue // no token issued yet: every link is 404
		}
		if sameToken(sum, t.TokenHash) && !matched {
			found, matched = t, true
		}
	}
	// A link past its end is no team, as a rotated one is: the page it
	// gets says neither which, nor that this one was ever real.
	if !matched || found.Expired(s.now()) {
		return Team{}, ErrNoTeam
	}
	return found, nil
}
