// Package teamportal is the Team portal page: the teams a magpie serves
// beside its own, and the link each team's lead signs in with.
//
// A team is a group of accounts of magpie's, and a gateway key held to
// that group: the portal beside magpie draws one team's accounts, and the
// key is what a member's client spends. The page makes both ends of that
// in one move — Ops.Add holds the key to the group as it records the team
// (internal/teamcore) — so the boundary a lead sees and the boundary the
// gateway enforces are the same list.
//
// Only the person who runs magpie comes here. A team's lead never sees
// this page: they are handed one link, /team/<token> on the portal, and
// everything they may do is behind it. That is why the page lives in
// magpie's own window, behind magpie's own sign-in, and not on the
// portal's port.
package teamportal

import (
	"github.com/yetone/magpie/internal/pluginrt"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/teamcore"
)

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "teamportal", API: "^1", Requires: []string{"window"},
		Name: "Team portal",
		Sub:  "Teams beside your own: each an account group and a key held to it, and the link its lead signs in with",
		One:  "1 team", Many: "{n} teams",
		InUse: InUse,
		// Models › Team portal, beside Gateway: the one place a team and
		// its link come into being
		Parts:  []pluginrt.Part{{Slot: "nav.page", Key: "teamportal"}},
		Client: []string{"i18n.js", "teamportal.css", "client.js"},
	})
}

// InUse counts the teams this magpie serves. A team with the page off
// would go on spending its key's accounts with nowhere to see it, so the
// page stays on while there is one. A folder that can't be read is not
// known, so it stays on too.
func InUse() (int, error) {
	if !teamcore.Served() {
		return 0, nil // no portal beside this magpie, so no teams
	}
	store, err := teamcore.NewStore(teamcore.Dir(settings.Dir()), 0)
	if err != nil {
		return 0, err
	}
	teams, err := store.Teams()
	if err != nil {
		return 0, err
	}
	return len(teams), nil
}
