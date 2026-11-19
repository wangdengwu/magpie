package teamd

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// The answers teamd reads from magpie's management API, as far as it needs
// them. They are teamd's own shapes, not magpie's packages: teamd is a
// separate program talking to a magpie over loopback, so what it reads is
// the wire. A change on magpie's side has to break a test here (there is
// one per call, on a fake magpie), never a page in production.

// Window is one allowance window of an account: how much of it is used,
// when it starts again, and when it was read.
type Window struct {
	Name     string     `json:"name"`
	Used     float64    `json:"used"` // percent, 0 to 100
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
	// Display is the vendor's own count ("1.2k / 3k") where it gives one.
	Display string `json:"display,omitempty"`
	// Text marks a row that is words, not a share (a plugin's balance):
	// its Display is the reading and Used is none.
	Text bool `json:"text,omitempty"`
	// Lane names an allowance apart from the account's own ("images").
	Lane string `json:"lane,omitempty"`
	// Unlimited is a window with no share to tell.
	Unlimited bool `json:"unlimited,omitempty"`
}

// Reading is one account's allowance as magpie last read it.
type Reading struct {
	Windows []Window `json:"windows"`
	// Error is why there is no reading: the vendor refused, or the
	// account has never answered.
	Error string `json:"error,omitempty"`
	// ReadAt is when this reading was made; nil when magpie doesn't say,
	// which is not the same as now.
	ReadAt *time.Time `json:"readAt,omitempty"`
	// AsOf is set when this is a reading kept from before, because the
	// vendor couldn't be asked this time: the number is real, its time is
	// older than the read.
	AsOf *time.Time `json:"asOf,omitempty"`
	// Deferred says no request was made this time, rather than one failed.
	Deferred bool `json:"readDeferred,omitempty"`
}

// Account is one account magpie has, as the portal shows it.
type Account struct {
	// ID is "<provider>/<stable id>", the entry an account group keeps.
	ID string
	// User is who is signed in: an email, or "email · Workspace".
	User string
	// Name is how the account is shown: the name ops gave it, else who is
	// signed in.
	Name string
	Plan string
	// ProviderName is the provider's name, "Claude".
	ProviderName string
	// Key is one of a provider's API keys rather than a signed-in
	// account: a group may hold either, and they are shown apart.
	Key bool
	// Agent is the agent whose sign-in magpie keeps this account's for
	// ("claude"), and User the sign-in itself: the two magpie's own routes
	// name an account by, where a group names it by the id it keeps.
	Agent string
	// SignedIn says magpie has a sign-in of this account, which is what
	// switching it on or off acts on. False for a key, and for an account
	// magpie lists without a sign-in of its own.
	SignedIn bool
	// On is the account magpie serves from: the active one, or one beside
	// it. Off is an account magpie was told to leave alone — the portal's
	// disable. Paused is the account its agent is signed in to, passed
	// over while another of the agent's is on: magpie can be made to serve
	// from it again without signing anything in.
	On     bool
	Off    bool
	Paused bool
}

// login is one of the accounts magpie remembers for an agent, as
// GET /providers lists it.
type login struct {
	User   string
	Plan   string
	Active bool
	On     bool
	Paused bool
}

// Catalog is what the portal needs to know about every account magpie has,
// put together from two calls:
//
//   - GET /gateway-keys/models lists every account and key magpie has with
//     the id a group keeps it by, the name it is shown with, and which of
//     the two it is. The id is magpie's own, so this is the only way to
//     read a group's entry as an account;
//   - GET /providers carries, per provider, the account its agent is
//     signed in to and the logins magpie remembers beside it, each with
//     whether it is in use.
//
// Keyed by the group's entry, in lower case, so a group's accounts join
// straight onto it.
type Catalog struct {
	Accounts map[string]Account
	// Logins are the accounts magpie remembers for an agent, by user.
	Logins map[string]map[string]login
}

// modelsAnswer is GET /gateway-keys/models, as far as teamd reads it.
type modelsAnswer struct {
	Accounts []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Provider     string `json:"provider"`
		ProviderName string `json:"providerName"`
		Account      bool   `json:"account"`
		Key          bool   `json:"key"`
		Plan         string `json:"plan,omitempty"`
	} `json:"accounts"`
}

// providersAnswer is GET /providers, as far as teamd reads it.
type providersAnswer struct {
	Providers []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Account *struct {
			Agent  string `json:"agent"`
			User   string `json:"user"`
			Plan   string `json:"plan,omitempty"`
			Logins []struct {
				User   string `json:"user"`
				Plan   string `json:"plan,omitempty"`
				Active bool   `json:"active"`
				On     bool   `json:"on"`
				Paused bool   `json:"paused,omitempty"`
			} `json:"logins,omitempty"`
		} `json:"account,omitempty"`
		// AccountNames are the names the user gave accounts of this
		// provider, by the account in lower case.
		AccountNames map[string]string `json:"accountNames,omitempty"`
	} `json:"providers"`
}

// Catalog reads every account and key magpie has, across every provider and
// every team. This is the answer that must never reach a browser: the
// portal filters it to one group's accounts and renders only those.
func (c *AdminClient) Catalog(ctx context.Context) (Catalog, error) {
	var models modelsAnswer
	if err := c.get(ctx, "gateway-keys/models", &models); err != nil {
		return Catalog{}, err
	}
	var provs providersAnswer
	if err := c.get(ctx, "providers", &provs); err != nil {
		return Catalog{}, err
	}
	out := Catalog{Accounts: make(map[string]Account, len(models.Accounts)), Logins: map[string]map[string]login{}}
	byProvider := map[string]int{}
	for i, p := range provs.Providers {
		byProvider[p.ID] = i
		logins := map[string]login{}
		for _, l := range p.Account.Logins {
			if l.User == "" {
				continue
			}
			logins[accountKey(l.User)] = login{User: l.User, Plan: l.Plan, Active: l.Active, On: l.On, Paused: l.Paused}
		}
		if p.Account != nil {
			// The account its agent is signed in to, when the logins list
			// doesn't carry it: it is in use by being the one signed in.
			if u := p.Account.User; u != "" {
				if _, had := logins[accountKey(u)]; !had {
					logins[accountKey(u)] = login{User: u, Plan: p.Account.Plan, Active: true, On: true}
				}
			}
		}
		out.Logins[p.ID] = logins
	}
	for _, e := range models.Accounts {
		acct := Account{
			ID: e.ID, Name: e.Name, Plan: e.Plan,
			ProviderName: e.ProviderName,
			Key:          e.Key || !e.Account,
		}
		pi, ok := byProvider[e.Provider]
		if !ok {
			out.Accounts[strings.ToLower(e.ID)] = acct
			continue
		}
		p := provs.Providers[pi]
		// the account the shown name is of: its user, which is what
		// magpie keys the allowance by
		user := shownUser(out.Logins[e.Provider], p.AccountNames, e.Name)
		acct.User, acct.Agent = user, e.Provider
		if l, known := out.Logins[e.Provider][accountKey(user)]; known && user != "" {
			// it has a sign-in of its own, which is what on and off act on,
			// and its own user rather than the name it is shown by
			acct.SignedIn, acct.User = true, l.User
			// paused is the account its agent is signed in to, passed over
			// while another is on: not in use, and not disabled
			acct.On = l.On && !l.Paused
			acct.Paused = l.Paused
			acct.Off = !l.On && !l.Active
			if acct.Plan == "" {
				acct.Plan = l.Plan
			}
		}
		out.Accounts[strings.ToLower(e.ID)] = acct
	}
	return out, nil
}

// shownUser is the account a shown name is of: the user itself when that is
// the name, else the one user the name was given to. "" when the name is
// two accounts', which is no account to read an allowance for.
func shownUser(logins map[string]login, names map[string]string, shown string) string {
	if _, ok := logins[accountKey(shown)]; ok {
		return shown
	}
	found := ""
	for user := range logins {
		if !strings.EqualFold(strings.TrimSpace(names[user]), shown) || shown == "" {
			continue
		}
		if found != "" && found != user {
			return ""
		}
		found = user
	}
	if found == "" {
		return shown
	}
	for user, l := range logins {
		if user == found {
			return l.User
		}
	}
	return found
}

// Usage are the allowance readings of one agent's accounts, by the
// account's name as magpie keys the reading: GET /login/usage.
//
// A reading magpie has never made has no entry, which is not an account at
// 0%.
func (c *AdminClient) Usage(ctx context.Context, agent string) (map[string]Reading, error) {
	var out map[string]Reading
	if err := c.get(ctx, "login/usage?agent="+url.QueryEscape(agent), &out); err != nil {
		return nil, err
	}
	byUser := make(map[string]Reading, len(out))
	for user, r := range out {
		byUser[accountKey(user)] = r
	}
	return byUser, nil
}
