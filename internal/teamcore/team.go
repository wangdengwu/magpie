package teamcore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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
	// ExpiresAt is when the token stops opening anything, zero for never.
	// A teams.json written before links had an end has none, so every
	// link issued then stays as it was: permanent.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// Expired is whether the team's link has reached its end at now. A link
// with no end never has.
func (t Team) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt)
}

// LinkLifetime is one lifetime a team's link may be issued for: the value
// a page posts and a command takes, and the lifetime itself, 0 for a link
// with no end.
type LinkLifetime struct {
	Value string
	TTL   time.Duration
}

// LinkLifetimes are the lifetimes a link is offered with, the page's and
// the command's alike. Forever is the default: a link issued without
// asking is what every link was before.
var LinkLifetimes = []LinkLifetime{
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
	{"90d", 90 * 24 * time.Hour},
	{LinkForever, 0},
}

// LinkForever is the lifetime of a link with no end.
const LinkForever = "forever"

// Lifetime is the value of the lifetime the team's link was issued for:
// LinkForever for one with no end, "" for an end none of LinkLifetimes
// gives (a link given its end by hand).
func (t Team) Lifetime() string {
	if t.ExpiresAt.IsZero() {
		return LinkForever
	}
	for _, l := range LinkLifetimes {
		if l.TTL > 0 && t.ExpiresAt.Sub(t.RotatedAt) == l.TTL {
			return l.Value
		}
	}
	return ""
}

// LinkTTL is the lifetime a value names, and whether it names one. ""
// is forever, so a caller that says nothing keeps the old behaviour.
func LinkTTL(v string) (time.Duration, bool) {
	if v == "" {
		return 0, true
	}
	for _, l := range LinkLifetimes {
		if l.Value == v {
			return l.TTL, true
		}
	}
	return 0, false
}

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
