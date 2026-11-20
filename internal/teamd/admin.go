package teamd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// AdminPath is magpie's management API: the path gateway.AdminPath serves
// on the other side of the loopback, and the only thing this client calls.
const AdminPath = "/v1/magpie/admin/"

// adminTimeout is how long one call may take. magpie answers these from
// its own files and, for a sign-in, the vendor's; a vendor that hangs must
// not hang a page's request forever.
const adminTimeout = 30 * time.Second

// maxAnswer is the most of a management API answer this client reads. The
// account list is the biggest one it asks for and is far under it.
const maxAnswer = 64 << 20

// Group is an account group as magpie lists it: a named set of accounts a
// gateway key may name as "@<id>". Its accounts are "<provider>/<stable
// id>", and one signed out since stays, matching nothing.
type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Accounts []string `json:"accounts"`
}

// AdminClient calls magpie's management API with the one key teamd holds.
//
// It is the only place that key is used. Its writes are not exported here:
// a change to magpie is made by a method that takes the Grant Authorize
// answers, so no code can act on an account without having resolved a
// team's link and checked the account against that team's group first.
type AdminClient struct {
	base string
	key  string
	hc   *http.Client
}

// NewAdminClient reads the admin key and builds the client. The key is
// read here, once, from a file: never from an argument or an environment
// variable, which every process on the machine can read.
func NewAdminClient(cfg Config) (*AdminClient, error) {
	base, err := loopbackBase(cfg.AdminURL)
	if err != nil {
		return nil, err
	}
	key, err := readAdminKey(cfg.AdminKeyFile)
	if err != nil {
		return nil, err
	}
	// A transport of this client's own. Its Proxy is left unset, so the
	// machine's proxy variables are not this client's either: magpie is on
	// loopback, in this Pod. And a dial that can't reach its own loopback
	// fails in seconds rather than at whatever the system's timeout is.
	tr := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &AdminClient{base: base, key: key, hc: &http.Client{Timeout: adminTimeout, Transport: tr}}, nil
}

// String is what a log line printing the client says: where it calls,
// never the key it calls with.
func (c *AdminClient) String() string { return "magpie's management API at " + c.base }

// readAdminKey reads path's key. A file holding more than one line is
// refused rather than guessed at: magpie would answer every call with 401
// and nothing would say why.
func readAdminKey(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("teamd needs the file holding magpie's admin key (-admin-key-file)")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("the admin key file can't be read: %w", err)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("the admin key file %s is empty", path)
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("the admin key file %s holds more than one key", path)
	}
	return key, nil
}

// MagpieError is magpie refusing a call, in its own words. A route's
// refusal is about the object the caller named — an account that is the
// only one in use, a group magpie has no such id for — and that is what a
// page has to say, where a bare "the management API answered 400" says
// nothing a lead can act on.
type MagpieError struct {
	Status  int
	Message string
}

func (e *MagpieError) Error() string {
	return fmt.Sprintf("magpie answered %d: %s", e.Status, e.Message)
}

// answerError is an answer that isn't 2xx: magpie's own error where it sent
// one, its body otherwise, with the admin key taken out of either.
func (c *AdminClient) answerError(what string, resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	text := oneLine(string(msg))
	var held struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(msg, &held) == nil && strings.TrimSpace(held.Error) != "" {
		text = oneLine(held.Error)
	}
	return fmt.Errorf("%s: %w", what, &MagpieError{Status: resp.StatusCode, Message: c.scrub(text)})
}

// GatewayKey is one of magpie's gateway keys as the management API lists
// it: the id ops hands out, the name it was given, and whether it is
// switched off. Its secret is not here and has nowhere to be: magpie sends
// only the masked form, which is enough to recognise a key and no use to
// use one.
//
// Its budget is in the two shapes magpie sends it in: a key's first limit
// at "limit" and its others at "moreLimits", and what it spent at "used"
// and "usedAll". Limits and Uses put each pair into one list.
type GatewayKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Off  bool   `json:"off"`
	// Models are what the key is held to: the models and providers it may
	// use, none for every model.
	Models     []string   `json:"models,omitempty"`
	Limit      *Limit     `json:"limit,omitempty"`
	MoreLimits []Limit    `json:"moreLimits,omitempty"`
	Used       *LimitUse  `json:"used,omitempty"`
	UsedAll    []LimitUse `json:"usedAll,omitempty"`
	// Accounts are what the key is held to (access.Key.Accounts):
	// "<provider>/<account>", "<provider>/!<account>" to keep one out, and
	// "@<group>" or "!@<group>" to name an account group. None holds the
	// key to nothing, which is every account magpie has — the opposite of
	// what it looks like, and why a team's key is checked for its group.
	Accounts []string `json:"accounts,omitempty"`
	// Issues says the key may issue keys under itself, which is what lets
	// the members page issue a team's keys.
	Issues bool `json:"issues,omitempty"`
	// Admin is the management level it holds, "" for none: a key that
	// manages magpie is never a team's.
	Admin string `json:"admin,omitempty"`
}

// Limits are the key's budgets in the order magpie keeps them. A request
// passes only while every one has something left, so a key with two caps is
// held by whichever runs out first. None for a key with no cap.
func (k GatewayKey) Limits() []Limit {
	out := make([]Limit, 0, 1+len(k.MoreLimits))
	if k.Limit != nil {
		out = append(out, *k.Limit)
	}
	return append(out, k.MoreLimits...)
}

// Uses is what the key spent of each of its Limits, in the same order. A
// magpie that sends only "used" (one limit, before a key could have more)
// reads the same as one that sends "usedAll".
func (k GatewayKey) Uses() []LimitUse {
	if len(k.UsedAll) > 0 {
		return k.UsedAll
	}
	if k.Used != nil {
		return []LimitUse{*k.Used}
	}
	return nil
}

// Limit is one of a key's budgets, as GET /gateway-keys reports it. teamd
// reads it as the wire, not as magpie's own type: this program talks to a
// magpie over loopback, so a change on magpie's side has to break a test
// here rather than a page in production.
type Limit struct {
	Period string `json:"period"`
	// Days is a "days" limit's cycle and Hours an "hours" one's: the two
	// that start again N of them after they began, rather than on a
	// calendar boundary.
	Days       int     `json:"days,omitempty"`
	Hours      int     `json:"hours,omitempty"`
	Tokens     int64   `json:"tokens,omitempty"`
	Cost       float64 `json:"cost,omitempty"`
	CacheReads bool    `json:"cacheReads,omitempty"`
	// Models scopes the limit to some models; none for the whole key.
	Models []string `json:"models,omitempty"`
	// Account and AccountWindow are an "account" limit's window: the key's
	// budget starts again when that account's window does.
	Account       string `json:"account,omitempty"`
	AccountWindow string `json:"accountWindow,omitempty"`
}

// LimitUse is what a key has spent of one limit. Tokens and Cost are what
// was counted, TokensLeft and CostLeft what is left of each cap, and Spent
// says the key is refused until Reset.
//
// Start and Reset are what magpie worked the window out to be when it
// answered. ResetUnknown says a reset couldn't be worked out then — an
// account limit whose account hasn't been read — and Reset is then zero,
// which is not the same as "resets now".
type LimitUse struct {
	Period        string    `json:"period"`
	Days          int       `json:"days,omitempty"`
	Hours         int       `json:"hours,omitempty"`
	Start         time.Time `json:"start"`
	Reset         time.Time `json:"reset"`
	Calls         int       `json:"calls"`
	Tokens        int64     `json:"tokens"`
	Cost          float64   `json:"cost"`
	Unpriced      int       `json:"unpriced,omitempty"`
	TokenLimit    int64     `json:"tokenLimit,omitempty"`
	CostLimit     float64   `json:"costLimit,omitempty"`
	CacheReads    bool      `json:"cacheReads,omitempty"`
	Models        []string  `json:"models,omitempty"`
	TokensLeft    int64     `json:"tokensLeft"`
	CostLeft      float64   `json:"costLeft"`
	Spent         bool      `json:"spent"`
	Account       string    `json:"account,omitempty"`
	AccountWindow string    `json:"accountWindow,omitempty"`
	ResetUnknown  bool      `json:"resetUnknown,omitempty"`
}

// GatewayKeys are magpie's gateway keys, for checking that the key a team
// was given is one magpie has, and for reading what the team has spent of
// it.
func (c *AdminClient) GatewayKeys(ctx context.Context) ([]GatewayKey, error) {
	var out struct {
		Keys []GatewayKey `json:"keys"`
	}
	if err := c.get(ctx, "gateway-keys", &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// AccountGroups are magpie's account groups as it has them now. Nothing is
// cached: a group ops changed takes effect on the next call, which is what
// the boundary rests on.
func (c *AdminClient) AccountGroups(ctx context.Context) ([]Group, error) {
	var out struct {
		Groups []Group `json:"groups"`
	}
	if err := c.get(ctx, "account-groups", &out); err != nil {
		return nil, err
	}
	return out.Groups, nil
}

// HoldKeyToGroup makes keyID's accounts list name the group and nothing
// else, which is what holds the key to that group's accounts (access's
// "accounts-key"). A key that names no account reaches every account
// magpie has, so this is what makes a team's boundary real rather than
// drawn.
//
// It replaces whatever the key named before. That is the point: the key
// is the team's, and what the team holds is its group, read as the group
// is now on every request (access.expandGroups). An account put on the
// key alone would outlive its place in the group, which is the boundary
// this is here to keep.
func (c *AdminClient) HoldKeyToGroup(ctx context.Context, keyID, groupID string) error {
	return c.post(ctx, "gateway-keys/accounts-key", map[string]any{
		"key":      keyID,
		"accounts": []string{"@" + strings.TrimSpace(groupID)},
	}, nil)
}

// post sends one management API path a JSON body and reads the answer into
// out. A 204, which several of these routes answer, leaves out untouched.
func (c *AdminClient) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+AdminPath+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.answerError("POST "+path, resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("POST %s: magpie's answer doesn't read: %w", path, err)
	}
	return nil
}

// get reads one management API path into out.
func (c *AdminClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+AdminPath+path, nil)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	// The key goes here, and no error below names a header: what a page, a
	// log line or an audit entry would otherwise hold.
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.answerError("GET "+path, resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(out); err != nil {
		return fmt.Errorf("GET %s: magpie's answer doesn't read: %w", path, err)
	}
	return nil
}

// scrub takes the key out of text magpie sent back, so that a reply which
// echoes it (a debug build, a proxy in between) can't put it in an error
// that reaches a page or a log.
func (c *AdminClient) scrub(s string) string {
	if c.key == "" {
		return s
	}
	return strings.ReplaceAll(s, c.key, "[admin key]")
}

// oneLine is s as one line, for an error.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// MemberKey is a key issued under a team's gateway key, as the members
// page shows it: the secret only on the answer that issued it.
type MemberKey struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Key     string    `json:"key,omitempty"`
	Expires time.Time `json:"expires"`
	// Issuer is the key it was issued under; magpie's own answers carry it.
	Issuer string `json:"issuer,omitempty"`
}

// IssueMemberKey issues a key under issuer, for a member named name, for
// ttl. The answer carries the secret, and that is the only answer that
// does.
func (c *AdminClient) IssueMemberKey(ctx context.Context, issuer, name string, ttl time.Duration) (MemberKey, error) {
	var out MemberKey
	err := c.post(ctx, "gateway-keys/issue-key", map[string]any{
		"issuer":     issuer,
		"name":       name,
		"ttlSeconds": int64(ttl / time.Second),
	}, &out)
	return out, err
}

// MemberKeys are the keys issued under issuer, without their secrets.
func (c *AdminClient) MemberKeys(ctx context.Context, issuer string) ([]MemberKey, error) {
	var out struct {
		Keys []MemberKey `json:"keys"`
	}
	if err := c.get(ctx, "gateway-keys/issued-keys?issuer="+url.QueryEscape(issuer), &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// RevokeMemberKey removes id, which must be one issued under issuer.
func (c *AdminClient) RevokeMemberKey(ctx context.Context, issuer, id string) error {
	return c.post(ctx, "gateway-keys/revoke-key", map[string]string{"issuer": issuer, "id": id}, nil)
}

// IssuerSpend is what one key a team's gateway key issued has used.
type IssuerSpend struct {
	KeyID  string  `json:"keyId"`
	Name   string  `json:"name"`
	Calls  int     `json:"calls"`
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   float64 `json:"cost"`
}

// IssuerUsage is what every key issued under one gateway key has spent over
// a period: today, the last 7 days, or the last 30.
//
// It asks by the issuer alone. magpie keeps the issuer on every record, so
// a key revoked since is still in the answer and still counts, which a list
// of live member ids would lose.
type IssuerUsage struct {
	Period string        `json:"period"`
	Since  time.Time     `json:"since"`
	Total  IssuerSpend   `json:"total"`
	Keys   []IssuerSpend `json:"keys"`
}

// IssuerUsage is what issuer's keys spent over period, "today", "7d" or
// "30d". It is a read: nothing in magpie changes.
func (c *AdminClient) IssuerUsage(ctx context.Context, issuer, period string) (IssuerUsage, error) {
	var out IssuerUsage
	q := "gateway-keys/issuer-usage?issuer=" + url.QueryEscape(issuer) + "&period=" + url.QueryEscape(period)
	if err := c.get(ctx, q, &out); err != nil {
		return IssuerUsage{}, err
	}
	return out, nil
}
