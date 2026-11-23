package teamd

import "github.com/yetone/magpie/internal/teamcore"

// What a team is, where teams are kept and how one is made are teamcore's:
// magpie's own window makes teams too, and it must not take the portal's
// pages with it. The portal reads them under the names it always had.
type (
	Store      = teamcore.Store
	Team       = teamcore.Team
	Entry      = teamcore.Entry
	Ops        = teamcore.Ops
	NewTeam    = teamcore.NewTeam
	Row        = teamcore.Row
	Removed    = teamcore.Removed
	Magpie     = teamcore.Magpie
	Group      = teamcore.Group
	GatewayKey = teamcore.GatewayKey
	Limit      = teamcore.Limit
	LimitUse   = teamcore.LimitUse
)

// the files the store keeps, under the names the portal's tests know them by
const (
	teamsFile = teamcore.TeamsFile
	auditFile = teamcore.AuditFile
	auditOld  = teamcore.AuditOld
)

const (
	DefaultAuditMax = teamcore.DefaultAuditMax
	OpsWho          = teamcore.OpsWho
	ResultOK        = teamcore.ResultOK
	ResultDenied    = teamcore.ResultDenied
)

var (
	NewStore   = teamcore.NewStore
	NewOpsWith = teamcore.NewOpsWith
	NewToken   = teamcore.NewToken
	HashToken  = teamcore.HashToken
	LinkTTL    = teamcore.LinkTTL
)

// NewOps opens the state folder and the admin key, as the portal does, for
// `teamd teams`.
func NewOps(cfg Config) (*Ops, error) {
	store, admin, err := open(cfg)
	if err != nil {
		return nil, err
	}
	return teamcore.NewOpsWith(store, admin), nil
}

// findGroup is the group with id, as teamcore finds it.
func findGroup(groups []Group, id string) (Group, bool) { return teamcore.FindGroup(groups, id) }
