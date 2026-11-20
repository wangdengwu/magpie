package teamcore

import "time"

// What a team is made of, as magpie's management API answers it: an
// account group, and a gateway key. These are the wire, read as it is
// sent, not as magpie's own types — the portal talks to a magpie over
// loopback, so a change on magpie's side has to break a test here rather
// than a page in production.
//
// They live in this package and not in the portal's: the ops page in
// magpie's own window reads the same two, and it must be the same two, or
// a key the page holds to a group would be a different key to the portal.

// Group is an account group as magpie lists it: a named set of accounts a
// gateway key may name as "@<id>". Its accounts are "<provider>/<stable
// id>", and one signed out since stays, matching nothing.
type Group struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Accounts []string `json:"accounts"`
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
