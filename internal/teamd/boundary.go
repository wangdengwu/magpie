package teamd

import (
	"crypto/subtle"
	"errors"
	"strings"
)

// Target is what an action means to touch: nothing (the team itself: the
// page that lists it), one account of the team's group, or that group.
// Every field an action names must pass the boundary; one it leaves out
// is not checked.
type Target struct {
	// Account is "<provider>/<stable id>", as an account group keeps an
	// account — not the name it is shown by.
	Account string
	// Group is an account group id.
	Group string
}

// Grant is what Authorize answers: the team a token named, and its account
// group as magpie reports it now. Its fields are unexported and Authorize
// is the only thing in the program that sets them, so a Grant a caller
// holds is one the boundary made. Every write to magpie takes one, which
// is how an action that never resolved a token and checked its target is
// kept from reaching magpie at all.
type Grant struct {
	team  Team
	group Group
}

// Team is the team the token named.
func (g Grant) Team() Team { return g.team }

// Group is that team's account group, as magpie reported it when the grant
// was made. It is a reading, not a promise: a page that acts does so
// through Authorize again, which reads the group again.
func (g Grant) Group() Group { return g.group }

// Holds says the grant's group holds account, "<provider>/<stable id>",
// told apart in lower case as magpie tells accounts apart.
func (g Grant) Holds(account string) bool {
	a, ok := cleanAccount(account)
	return ok && slicesContainsFold(g.group.Accounts, a)
}

// Accounts is the group's accounts as magpie reports them, a copy.
func (g Grant) Accounts() []string {
	if len(g.group.Accounts) == 0 {
		return nil
	}
	out := make([]string, len(g.group.Accounts))
	copy(out, g.group.Accounts)
	return out
}

// The answers a refused action gets, and nothing more: a link that names
// no team says nothing about why (a chat client that cut a token short
// must not learn which part was wrong), and a target outside the team says
// nothing about where it is.
var (
	// ErrNoTeam is a token never issued or rotated out.
	ErrNoTeam = errors.New("this link names no team")
	// ErrNotYours is a target the team's group doesn't hold.
	ErrNotYours = errors.New("that is not this team's")
	// ErrNoGroup is a team whose account group magpie doesn't have: ops
	// hasn't made it, or has removed it.
	ErrNoGroup = errors.New("this team's account group is not in magpie")
)

// sameToken says two hashes are of one token, in constant time. Two empty
// hashes never match: a team whose token hasn't been issued holds no hash,
// and an empty one must not open it.
func sameToken(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// cleanAccount is account as a group keeps it, "<provider>/<stable id>",
// told apart in lower case; false for a shape no group holds — empty, with
// no "/" or more than one, or naming a group ("@id") or an account kept
// out ("!id"), which are key-list marks and never accounts.
func cleanAccount(account string) (string, bool) {
	a := strings.ToLower(strings.TrimSpace(account))
	if a == "" || strings.ContainsAny(a, "@!") {
		return "", false
	}
	p, r, ok := strings.Cut(a, "/")
	if !ok || p == "" || r == "" || strings.Contains(r, "/") {
		return "", false
	}
	return a, true
}

// accountKey is who is signed in as magpie compares it: trimmed, lower
// case. Magpie keys a saved account and its allowance by this, so the two
// sides of a group entry join on it.
func accountKey(user string) string { return strings.ToLower(strings.TrimSpace(user)) }

// slicesContainsFold is slices.ContainsFunc(EqualFold), which the group's
// accounts and a target are compared with.
func slicesContainsFold(list []string, v string) bool {
	for _, a := range list {
		if strings.EqualFold(strings.TrimSpace(a), v) {
			return true
		}
	}
	return false
}
