package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/teamcore"
)

// The Team portal page's routes (plugins/teamportal draws the page): the
// one place a team and its link come into being.
//
// They run in magpie's process, over teamd's files and magpie's own groups
// and keys, with no key of their own: the page is already behind magpie
// web's sign-in, and a portal's admin key exists only for the portal's
// HTTP calls. They are not under /api, so the management API never reaches
// them, and a guest of the app's windows (a phone, a box) never sees them.

// guiMagpie is magpie's own reading of what ops asks, in process.
type guiMagpie struct{}

func (guiMagpie) AccountGroups(context.Context) ([]teamcore.Group, error) {
	gs, err := access.AccountGroups()
	if err != nil {
		return nil, err
	}
	out := make([]teamcore.Group, 0, len(gs))
	for _, g := range gs {
		out = append(out, teamcore.Group{ID: g.ID, Name: g.Name, Accounts: g.Accounts})
	}
	return out, nil
}

func (guiMagpie) GatewayKeys(context.Context) ([]teamcore.GatewayKey, error) {
	keys, err := access.List()
	if err != nil {
		return nil, err
	}
	out := make([]teamcore.GatewayKey, 0, len(keys))
	for _, k := range keys {
		// Accounts travels with the key: Ops.Add reads it to know whether
		// the key is already held to its group, which without it looks like
		// no key is (a field left out here is the check reading an empty
		// list, and the page holding every key again).
		out = append(out, teamcore.GatewayKey{ID: k.ID, Name: k.Name, Off: k.Off, Accounts: k.Accounts, Issues: k.Issues, Admin: k.Admin})
	}
	return out, nil
}

// HoldKeyToGroup is accounts-key in this process: the same call the
// Gateway page's own key menu makes, with no key and no HTTP.
func (guiMagpie) HoldKeyToGroup(_ context.Context, keyID, groupID string) error {
	_, err := access.Update("accounts-key", access.Change{Key: keyID, Accounts: []string{"@" + strings.TrimSpace(groupID)}})
	return err
}

// opsRoutes are the page's routes, each answering JSON. The store is
// teamd's own folder on the volume, opened here with the same lock teamd
// takes.
func opsRoutes(store *teamcore.Store, magpie teamcore.Magpie) http.Handler {
	ops := teamcore.NewOpsWith(store, magpie)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ops/teams/list", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := requestContext(r)
		defer cancel()
		opsJSON(w, http.StatusOK, opsListOf(ctx, ops))
	})
	// each write: the page's own, a JSON body naming what it acts on
	write := func(path string, act func(ctx context.Context, in opsAsk) (any, error)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			if !opsOwnPage(r) {
				opsJSON(w, http.StatusForbidden, opsErr{"this is asked from magpie's own page"})
				return
			}
			var in opsAsk
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
				opsJSON(w, http.StatusBadRequest, opsErr{"the request didn't read"})
				return
			}
			ctx, cancel := requestContext(r)
			defer cancel()
			out, err := act(ctx, in)
			if err != nil {
				opsJSON(w, http.StatusBadRequest, opsErr{err.Error()})
				return
			}
			opsJSON(w, http.StatusOK, out)
		})
	}
	// a team made, its key held to its group, and its link: the only time
	// the link exists in the clear
	write("/ops/teams/add", func(ctx context.Context, in opsAsk) (any, error) {
		team, err := ops.Add(ctx, teamcore.NewTeam{Name: strings.TrimSpace(in.Name), GroupID: strings.TrimSpace(in.Group), KeyID: strings.TrimSpace(in.Key)})
		if err != nil {
			return nil, err
		}
		token, _, err := ops.Token(team.ID, 0)
		if err != nil {
			return nil, errors.New("the team was made, but its link couldn't be: " + err.Error())
		}
		return opsLink{ID: team.ID, Name: team.Name, Link: "/team/" + token}, nil
	})
	// a new link, which cuts the old one off
	write("/ops/teams/token", func(ctx context.Context, in opsAsk) (any, error) {
		token, team, err := ops.Token(strings.TrimSpace(in.ID), 0)
		if err != nil {
			return nil, err
		}
		return opsLink{ID: team.ID, Name: team.Name, Link: "/team/" + token}, nil
	})
	// a team made before Add held its key, held now
	write("/ops/teams/hold", func(ctx context.Context, in opsAsk) (any, error) {
		team, err := ops.Hold(ctx, strings.TrimSpace(in.ID))
		if err != nil {
			return nil, err
		}
		return opsLink{ID: team.ID, Name: team.Name}, nil
	})
	// the team forgotten; its group and its key are left as they are, and
	// the answer says how many accounts the group still holds
	write("/ops/teams/remove", func(ctx context.Context, in opsAsk) (any, error) {
		gone, err := ops.Remove(ctx, strings.TrimSpace(in.ID), true)
		if err != nil {
			return nil, err
		}
		return opsRemoved{ID: gone.Team.ID, Left: len(gone.Accounts())}, nil
	})
	return mux
}

// opsAsk is a write's body: the fields each write reads.
type opsAsk struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
	Key   string `json:"key"`
}

type opsErr struct {
	Error string `json:"error"`
}

// opsLink is a team, and its link when the write made one: a path the
// page puts its own address in front of.
type opsLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Link string `json:"link,omitempty"`
}

type opsRemoved struct {
	ID   string `json:"id"`
	Left int    `json:"left"`
}

// opsList is what the page draws: the teams, and magpie's own groups and
// keys to make one from. Err is what couldn't be read, the rest drawn as
// far as it was.
type opsList struct {
	Teams  []opsTeam  `json:"teams"`
	Groups []opsGroup `json:"groups"`
	Keys   []opsKey   `json:"keys"`
	Err    string     `json:"error,omitempty"`
}

type opsTeam struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName,omitempty"`
	KeyID     string `json:"keyId"`
	KeyName   string `json:"keyName,omitempty"`
	// Linked: a link has been issued; the link itself is never kept
	Linked bool `json:"linked"`
	// Accounts is how many accounts its group holds, nil when magpie
	// couldn't say
	Accounts *int `json:"accounts"`
	// Held: its key names its group. A team made before Add held keys may
	// have one that reaches every account magpie has, which the page says,
	// with a way to hold it; nil when the key couldn't be read
	Held *bool `json:"held"`
}

type opsGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Accounts int    `json:"accounts"`
}

// opsKey is a key a team may be made on: switched on, managing nothing.
type opsKey struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Issues bool   `json:"issues"`
}

func opsListOf(ctx context.Context, ops *teamcore.Ops) opsList {
	out := opsList{Teams: []opsTeam{}, Groups: []opsGroup{}, Keys: []opsKey{}}
	var errs []string
	rows, err := ops.List(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	groups, gErr := ops.Groups(ctx)
	if gErr != nil {
		errs = append(errs, gErr.Error())
	}
	keys, kErr := ops.Keys(ctx)
	if kErr != nil {
		errs = append(errs, kErr.Error())
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, opsGroup{ID: g.ID, Name: g.Name, Accounts: len(g.Accounts)})
	}
	for _, k := range keys {
		if k.Off || k.Admin != "" {
			continue // a key that manages magpie is never a team's
		}
		out.Keys = append(out.Keys, opsKey{ID: k.ID, Name: k.Name, Issues: k.Issues})
	}
	for _, row := range rows {
		t := opsTeam{ID: row.Team.ID, Name: row.Team.Name, GroupID: row.Team.GroupID, KeyID: row.Team.KeyID, Linked: row.Team.TokenHash != ""}
		if row.Known {
			n := row.Accounts
			t.Accounts = &n
		}
		if g, ok := teamcore.FindGroup(groups, row.Team.GroupID); ok {
			t.GroupName = g.Name
		}
		if kErr == nil {
			held := false
			if i := slices.IndexFunc(keys, func(k teamcore.GatewayKey) bool { return strings.EqualFold(k.ID, row.Team.KeyID) }); i >= 0 {
				t.KeyName = keys[i].Name
				held = teamcore.HoldsGroup(keys[i].Accounts, row.Team.GroupID)
			}
			t.Held = &held
		}
		out.Teams = append(out.Teams, t)
	}
	out.Err = strings.Join(errs, "; ")
	return out
}

func opsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// opsOwnPage turns away a write another site's page sent: the page's
// cookie is SameSite=Lax, so a sibling name behind the same proxy could
// send one, as webOwnPage says of every write of the app's.
func opsOwnPage(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == "http://"+r.Host || r.Header.Get("Origin") == "https://"+r.Host
}

// requestContext bounds one action the way the portal's own do.
func requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

// teamdOpsDir is where teamd keeps its teams, as the sidecar was given it:
// the same folder on the volume, so the page and the portal read one file.
// MAGPIE_TEAMD_DIR names another, as a run outside the container has.
func teamdOpsDir() string { return teamcore.Dir(settings.Dir()) }
