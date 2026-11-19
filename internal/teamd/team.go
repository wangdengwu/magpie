package teamd

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Team is one team the portal serves: the account group ops made for it,
// the gateway key its members use, and the hash of the token that opens
// its pages. The token itself is never kept — a link lost is rotated, not
// recovered.
type Team struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	GroupID string `json:"groupId"`
	KeyID   string `json:"keyId"`
	// TokenHash is HashToken of the team's token, empty for a team whose
	// token hasn't been issued yet: every page answers that team's links
	// with 404 until one is.
	TokenHash string    `json:"tokenHash,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	RotatedAt time.Time `json:"rotatedAt,omitzero"`
}

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

// tokenBytes is how much randomness a team token carries: 256 bits, far
// past guessing, and short enough to sit in a URL a person pastes.
const tokenBytes = 32

// NewToken makes a team token and the hash kept of it. The token is the
// only copy: it is printed once, when the team is given its link.
func NewToken() (token, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("a team token can't be made: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is what teams.json keeps of a token. A fast hash is enough
// here, unlike for a password: the token is 256 random bits, so there is
// nothing to guess and no dictionary to try.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte("teamd token\n" + token))
	return hex.EncodeToString(sum[:])
}

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
