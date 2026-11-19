package gui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
)

// Member keys through the management API: a team portal holds one
// gateway key that may issue keys (Issues), and hands each of a team's
// members a key of their own. The portal names the issuer because it
// serves more than one team; what limits it is that access.IssueKey
// bounds every key it makes by that issuer — its models, its accounts,
// its limits and its life — and that revoking reaches only the issuer's
// own.
//
// Nothing here can make a key an issuer or an admin: those are the app's
// and the command line's, and access.Update refuses them for a key that
// was issued.

// adminIssueKey answers POST /gateway-keys/issue-key.
//
//	{"issuer":"<key id>","name":"Alice","ttlSeconds":604800,
//	 "tokens":0,"cost":0,"models":["claude/*"]}
func adminIssueKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issuer   string          `json:"issuer"`
		Name     string          `json:"name"`
		TTL      json.RawMessage `json:"ttlSeconds"`
		Models   []string        `json:"models"`
		Accounts []string        `json:"accounts"`
		Tokens   int64           `json:"tokens"`
		Cost     float64         `json:"cost"`
		ReadOnly bool            `json:"readOnly"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		adminError(w, http.StatusBadRequest, "the body is a JSON object: {\"issuer\": \"<id>\", \"name\": \"Alice\", \"ttlSeconds\": 604800}")
		return
	}
	if err := access.IssueUnder(in.Issuer); err != nil {
		adminError(w, http.StatusBadRequest, err.Error())
		return
	}
	ttl, err := adminTTL(in.TTL)
	if err != nil {
		adminError(w, http.StatusBadRequest, err.Error())
		return
	}
	k, err := access.IssueKey(access.Issue{
		Name: in.Name, TTL: ttl, Models: in.Models, Accounts: in.Accounts,
		Tokens: in.Tokens, Cost: in.Cost, ReadOnly: in.ReadOnly, Issuer: in.Issuer,
	})
	if err != nil {
		adminError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, k)
}

// adminIssuedKeys answers GET /gateway-keys/issued-keys?issuer=<id>: the
// keys issued under it, never a secret.
func adminIssuedKeys(w http.ResponseWriter, r *http.Request) {
	issuer := strings.TrimSpace(r.URL.Query().Get("issuer"))
	if issuer == "" {
		adminError(w, http.StatusBadRequest, "name the issuer: ?issuer=<key id>")
		return
	}
	keys, err := access.IssuedBy(issuer)
	if err != nil {
		adminError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		e := map[string]any{"id": k.ID, "name": k.Name, "key": k.Masked, "expires": k.Expires}
		if len(k.Models) > 0 {
			e["models"] = k.Models
		}
		if len(k.Accounts) > 0 {
			e["accounts"] = k.Accounts
		}
		if k.Limit != nil {
			e["limit"] = k.Limit
		}
		out = append(out, e)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"keys": out})
}

// adminRevokeKey answers POST /gateway-keys/revoke-key
// {"issuer":"<id>","id":"<id>"}. Another issuer's key is answered as one
// that isn't there, and is left working.
func adminRevokeKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Issuer string `json:"issuer"`
		ID     string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		adminError(w, http.StatusBadRequest, "the body is a JSON object: {\"issuer\": \"<id>\", \"id\": \"<id>\"}")
		return
	}
	if err := access.RevokeIssued(strings.TrimSpace(in.ID), strings.TrimSpace(in.Issuer)); err != nil {
		adminError(w, http.StatusNotFound, "no key issued under that issuer has the id "+strconv.Quote(in.ID))
		return
	}
	writeJSON(w, map[string]any{"revoked": in.ID})
}

// adminTTL reads ttlSeconds, a whole number of seconds (a JSON string is
// taken as one too). The cap is the one the constants hold.
func adminTTL(raw json.RawMessage) (time.Duration, error) {
	bad := errors.New("ttlSeconds is how long the key lives, in seconds, up to " +
		strconv.Itoa(int(access.MaxIssuedTTL.Hours()/24)) + " days")
	if len(raw) == 0 {
		return 0, bad
	}
	var n int64
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0, bad
		}
		v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return 0, bad
		}
		n = v
	}
	d := time.Duration(n) * time.Second
	if d < access.MinTTL || d > access.MaxIssuedTTL {
		return 0, bad
	}
	return d, nil
}
