package teamcore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Ops is what `teamd teams` does: the only way a team, and the only way a
// team token, comes into existence. It writes teamd's own files and reads
// magpie's groups and keys; it never changes magpie.
type Ops struct {
	store *Store
	cat   Magpie
	now   func() time.Time
}

// Magpie is what ops reads of magpie, and the one thing it writes: the
// account groups and the gateway keys. The portal's admin client is one;
// magpie's own page is another, reading them in its own process with no
// key at all.
type Magpie interface {
	AccountGroups(ctx context.Context) ([]Group, error)
	GatewayKeys(ctx context.Context) ([]GatewayKey, error)
	// HoldKeyToGroup makes a key name an account group and nothing else,
	// which is what holds it to that group's accounts. Add calls it for a
	// key that names none, so a team made from the page is held as the
	// portal draws it.
	HoldKeyToGroup(ctx context.Context, keyID, groupID string) error
}

// Groups and Keys are what magpie has now, as ops reads them.
func (o *Ops) Groups(ctx context.Context) ([]Group, error)    { return o.cat.AccountGroups(ctx) }
func (o *Ops) Keys(ctx context.Context) ([]GatewayKey, error) { return o.cat.GatewayKeys(ctx) }

// NewOpsWith is ops over a store this process already holds, reading
// magpie in process rather than over the management API.
func NewOpsWith(store *Store, cat Magpie) *Ops {
	return &Ops{store: store, cat: cat, now: time.Now}
}

// NewOpsAt is NewOpsWith on a clock of the caller's: a team's CreatedAt and
// a token's RotatedAt are read from it.
func NewOpsAt(store *Store, cat Magpie, now func() time.Time) *Ops {
	return &Ops{store: store, cat: cat, now: now}
}

// NewTeam is what ops gives `teams add`: the name the team goes by, and the
// account group and gateway key ops already made for it in magpie.
type NewTeam struct {
	Name    string
	GroupID string
	KeyID   string
}

// Add records a team, after checking in magpie that the group and the key
// are real and that no other team has either. Nothing is written when a
// check fails, and the team it makes has no token yet: `teams token`
// issues one.
func (o *Ops) Add(ctx context.Context, in NewTeam) (Team, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Team{}, errors.New("a team needs a name")
	}
	if utf8.RuneCountInString(name) > 80 {
		return Team{}, errors.New("a team's name is at most 80 characters")
	}
	id, err := teamID(name)
	if err != nil {
		return Team{}, err
	}
	groups, err := o.cat.AccountGroups(ctx)
	if err != nil {
		return Team{}, err
	}
	group, ok := FindGroup(groups, in.GroupID)
	if !ok {
		return Team{}, fmt.Errorf("magpie has no account group %q: make it first, with `magpie gateway-key groups` or the app", in.GroupID)
	}
	keys, err := o.cat.GatewayKeys(ctx)
	if err != nil {
		return Team{}, err
	}
	ki := slices.IndexFunc(keys, func(k GatewayKey) bool { return strings.EqualFold(k.ID, strings.TrimSpace(in.KeyID)) })
	if ki < 0 {
		return Team{}, fmt.Errorf("magpie has no gateway key %q: make it first, with `magpie gateway-key add %s`", in.KeyID, id)
	}
	if keys[ki].Off {
		return Team{}, fmt.Errorf("the gateway key %q is switched off: the team would reach nothing", in.KeyID)
	}
	if keys[ki].Admin != "" {
		return Team{}, fmt.Errorf("the gateway key %q manages magpie, so it can't be a team's: a team's key reaches its own accounts only", in.KeyID)
	}
	// Another team's group or key is answered first: which of the two is
	// wrong is a thing about this team, where a key held to nothing is a
	// thing about the key, and ops reads the more specific one first.
	taken, err := o.store.Teams()
	if err != nil {
		return Team{}, err
	}
	for _, t := range taken {
		if strings.EqualFold(t.ID, id) {
			return Team{}, fmt.Errorf("a team %q is here already: take it away with `teamd teams rm %s` first", t.ID, t.ID)
		}
		if strings.EqualFold(t.GroupID, group.ID) {
			return Team{}, fmt.Errorf("the account group %s is team %q's already: one group is one team's", group.ID, t.ID)
		}
		if strings.EqualFold(t.KeyID, keys[ki].ID) {
			return Team{}, fmt.Errorf("the gateway key %s is team %q's already: one key is one team's", keys[ki].ID, t.ID)
		}
	}
	// A key that names no account reaches every account magpie has, every
	// provider's, whatever group the team holds: the portal would draw one
	// group and the key would spend all of them. The group is put on the
	// key here, so a team made from a page is held as the portal draws it
	// — the two never come apart, which is what a page that asked ops to
	// run a command could not promise.
	//
	// A key already held to the group is left as it is: it may name other
	// accounts of its own, which that team's ops meant to allow.
	if !HoldsGroup(keys[ki].Accounts, group.ID) {
		if err := o.cat.HoldKeyToGroup(ctx, keys[ki].ID, group.ID); err != nil {
			return Team{}, fmt.Errorf("the team's key couldn't be held to its account group %s: %w", group.ID, err)
		}
	}
	made := Team{
		ID:        id,
		Name:      name,
		GroupID:   group.ID,
		KeyID:     keys[ki].ID,
		CreatedAt: o.now().UTC(),
	}
	// the write below takes the lock and checks all three again: this read
	// is only so the more specific refusal is the one ops sees
	err = o.store.Update(func(teams []Team) ([]Team, error) {
		for _, t := range teams {
			if strings.EqualFold(t.ID, made.ID) {
				return nil, fmt.Errorf("a team %q is here already: take it away with `teamd teams rm %s` first", t.ID, t.ID)
			}
			if strings.EqualFold(t.GroupID, made.GroupID) {
				return nil, fmt.Errorf("the account group %s is team %q's already: one group is one team's", made.GroupID, t.ID)
			}
			if strings.EqualFold(t.KeyID, made.KeyID) {
				return nil, fmt.Errorf("the gateway key %s is team %q's already: one key is one team's", made.KeyID, t.ID)
			}
		}
		return append(teams, made), nil
	})
	if err != nil {
		return Team{}, err
	}
	_ = o.store.Audit(Entry{Team: made.ID, Action: "team-add", Who: OpsWho, Result: "ok"})
	return made, nil
}

// Row is one line of `teamd teams list`.
type Row struct {
	Team Team
	// Accounts is how many accounts magpie's group holds, and Known
	// whether magpie answered at all: a list that can't reach magpie still
	// says which teams exist.
	Accounts int
	Known    bool
}

// List is the teams as they stand, with what magpie says each one's group
// holds. A magpie that can't be read leaves the counts unknown rather than
// failing the listing.
func (o *Ops) List(ctx context.Context) ([]Row, error) {
	teams, err := o.store.Teams()
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(teams))
	groups, groupsErr := o.cat.AccountGroups(ctx)
	for _, t := range teams {
		row := Row{Team: t}
		if groupsErr == nil {
			if g, ok := FindGroup(groups, t.GroupID); ok {
				row.Accounts, row.Known = len(g.Accounts), true
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Hold holds team id's key to its account group, as Add does for a team
// it makes: for a team made before Add did, whose key may still reach every
// account magpie has. A key that names the group already is left as it is.
func (o *Ops) Hold(ctx context.Context, id string) (Team, error) {
	teams, err := o.store.Teams()
	if err != nil {
		return Team{}, err
	}
	i := slices.IndexFunc(teams, func(t Team) bool { return strings.EqualFold(t.ID, strings.TrimSpace(id)) })
	if i < 0 {
		return Team{}, fmt.Errorf("there is no team %q", id)
	}
	team := teams[i]
	keys, err := o.cat.GatewayKeys(ctx)
	if err != nil {
		return Team{}, err
	}
	ki := slices.IndexFunc(keys, func(k GatewayKey) bool { return strings.EqualFold(k.ID, team.KeyID) })
	if ki < 0 {
		return Team{}, fmt.Errorf("magpie has no gateway key %s, team %q's", team.KeyID, team.ID)
	}
	if HoldsGroup(keys[ki].Accounts, team.GroupID) {
		return team, nil
	}
	if err := o.cat.HoldKeyToGroup(ctx, team.KeyID, team.GroupID); err != nil {
		return Team{}, fmt.Errorf("the team's key couldn't be held to its account group %s: %w", team.GroupID, err)
	}
	_ = o.store.Audit(Entry{Team: team.ID, Action: "team-hold", Who: OpsWho, Result: ResultOK})
	return team, nil
}

// Token issues a team a new token, or replaces the one it had, and answers
// it. This is the only time the token exists in the clear: teams.json keeps
// its hash, so a link lost is rotated here, never recovered. Replacing it
// kills the old link at once.
//
// ttl is how long the link opens the team's pages, counted from now; 0 is
// a link with no end, as every link was before they had one.
func (o *Ops) Token(id string, ttl time.Duration) (token string, team Team, err error) {
	token, hash, err := NewToken()
	if err != nil {
		return "", Team{}, err
	}
	at := o.now().UTC()
	var ends time.Time
	if ttl > 0 {
		ends = at.Add(ttl)
	}
	err = o.store.Update(func(teams []Team) ([]Team, error) {
		i := slices.IndexFunc(teams, func(t Team) bool { return strings.EqualFold(t.ID, strings.TrimSpace(id)) })
		if i < 0 {
			return nil, fmt.Errorf("there is no team %q: `teamd teams list` names the ones there are", id)
		}
		teams[i].TokenHash = hash
		teams[i].RotatedAt = at
		teams[i].ExpiresAt = ends
		team = teams[i]
		return teams, nil
	})
	if err != nil {
		return "", Team{}, err
	}
	_ = o.store.Audit(Entry{Team: team.ID, Action: "team-token", Who: OpsWho, Result: "ok"})
	return token, team, nil
}

// Removed is what a `teams rm` took away, and what it left in magpie.
type Removed struct {
	Team Team
	// Group is magpie's group as it was read, and Read whether it could be
	// read at all: a forced removal of a team whose group couldn't be read
	// says nothing about what is still in it.
	Group Group
	Read  bool
}

// Accounts are the accounts the removal left in magpie's group.
func (r Removed) Accounts() []string { return r.Group.Accounts }

// Remove forgets a team: its name, its group, its key and its token. It
// changes nothing in magpie, so the group and the key stay as they are.
//
// A team whose group still holds accounts is refused, and the accounts it
// would leave behind are named: forgetting the team would leave a pool
// nobody can manage through the portal. force goes ahead anyway, which is
// what ops wants when the team is gone and the accounts are being handed
// to another.
func (o *Ops) Remove(ctx context.Context, id string, force bool) (Removed, error) {
	teams, err := o.store.Teams()
	if err != nil {
		return Removed{}, err
	}
	i := slices.IndexFunc(teams, func(t Team) bool { return strings.EqualFold(t.ID, strings.TrimSpace(id)) })
	if i < 0 {
		return Removed{}, fmt.Errorf("there is no team %q: `teamd teams list` names the ones there are", id)
	}
	out := Removed{Team: teams[i]}
	groups, groupsErr := o.cat.AccountGroups(ctx)
	if groupsErr == nil {
		out.Group, out.Read = FindGroup(groups, out.Team.GroupID)
	}
	if !force {
		switch {
		case groupsErr != nil:
			return Removed{}, fmt.Errorf("magpie's groups can't be read, so what the team still holds can't be said (%v): say --force to forget it anyway", groupsErr)
		case !out.Read:
			return Removed{}, fmt.Errorf("magpie has no account group %s any more: say --force to forget the team", out.Team.GroupID)
		case len(out.Group.Accounts) > 0:
			return Removed{}, fmt.Errorf("the team %s still holds %d accounts in magpie (%s): take them out of the group first, or say --force to forget the team and leave them", out.Team.ID, len(out.Group.Accounts), strings.Join(out.Group.Accounts, ", "))
		}
	}
	err = o.store.Update(func(teams []Team) ([]Team, error) {
		i := slices.IndexFunc(teams, func(t Team) bool { return strings.EqualFold(t.ID, out.Team.ID) })
		if i < 0 {
			return nil, fmt.Errorf("the team %q was taken away meanwhile", out.Team.ID)
		}
		return slices.Delete(teams, i, i+1), nil
	})
	if err != nil {
		return Removed{}, err
	}
	_ = o.store.Audit(Entry{Team: out.Team.ID, Action: "team-rm", Who: OpsWho, Result: "ok"})
	return out, nil
}

// HoldsGroup says a key's accounts list names the group id, as "@<id>",
// which is what holds the key to the group's accounts and no others. A
// "!@<id>" is the group kept out, the opposite, so it doesn't count. An
// empty list names nothing, and such a key reaches every account magpie
// has — the reason a team's key is checked for this at all.
func HoldsGroup(accounts []string, id string) bool {
	want := "@" + strings.TrimSpace(id)
	for _, a := range accounts {
		if strings.EqualFold(strings.TrimSpace(a), want) {
			return true
		}
	}
	return false
}

// teamID is the id a team goes by in a link and on a command line: its
// name in lower case, with anything that isn't a letter or a digit turned
// into one "-", at most 40 characters. A name with no Latin letters or
// digits in it has no id, and says so.
func teamID(name string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "", fmt.Errorf("the name %q has no Latin letters or digits to make a team id of: give it a name that has some", name)
	}
	return id, nil
}
