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
