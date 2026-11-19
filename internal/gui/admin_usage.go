package gui

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/usage"
)

// What each key issued under a gateway key has spent, through the
// management API: a team portal reads how much each of a team's member keys
// used, over a period, without a route of its own to magpie's Usage page.
//
// The filter is the issuer, not a list of member keys. Every usage record
// carries the key it was made with and the key that one was issued under
// (usage.Record.CallerKeyID, CallerIssuerID), so a key revoked since is
// still in the ledger and still counts under its issuer — a list of live
// ids would lose the history a lead is asking for.
//
// Read-only: nothing is written, and no secret is in the answer. A record
// written before CallerIssuerID was kept has it empty, so it belongs to no
// issuer and is never swept into a total.

// adminIssuerUsage answers
// GET /gateway-keys/issuer-usage?issuer=<key id>&period=today|7d|30d.
func adminIssuerUsage(w http.ResponseWriter, r *http.Request) {
	issuer := strings.TrimSpace(r.URL.Query().Get("issuer"))
	if issuer == "" {
		adminError(w, http.StatusBadRequest, "name the issuer: ?issuer=<key id>&period=today|7d|30d")
		return
	}
	period := usage.Period(strings.TrimSpace(r.URL.Query().Get("period")))
	if !slices.Contains([]usage.Period{usage.Today, usage.Week, usage.Month}, period) {
		adminError(w, http.StatusBadRequest, "name the period: ?period=today, 7d or 30d")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, issuerUsage(issuer, period, usage.Clock()))
}

// issuerSpend is what one key issued under the issuer used.
type issuerSpend struct {
	KeyID  string  `json:"keyId"`
	Name   string  `json:"name"`
	Calls  int     `json:"calls"`
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   float64 `json:"cost"`
}

// issuerUsageTotals is the same four figures for the whole issuer.
type issuerUsageTotals struct {
	Calls  int     `json:"calls"`
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Cost   float64 `json:"cost"`
}

// issuerUsageAnswer is the route's answer, and what teamd reads.
type issuerUsageAnswer struct {
	Period string            `json:"period"`
	Since  time.Time         `json:"since"`
	Total  issuerUsageTotals `json:"total"`
	Keys   []issuerSpend     `json:"keys"`
}

// issuerUsage sums the period's records that were made with a key issued
// under issuer, by that key.
//
// The counting is the ledger's, not this route's: a try handed on is no
// request (Record.HandedOn), and a call magpie turned away is not one
// either (Record.IsRejected) — the same rules usage.Summarize counts by, so
// the figures here are the ones magpie's own Usage page shows for the team
// key.
func issuerUsage(issuer string, period usage.Period, now time.Time) issuerUsageAnswer {
	out := issuerUsageAnswer{Period: string(period), Since: period.Since(now), Keys: []issuerSpend{}}
	sums := map[string]*issuerSpend{}
	cost := usage.NewCoster()
	usage.Visit(out.Since, func(rec usage.Record) {
		if rec.IsRejected() || rec.CallerKeyID == "" || rec.CallerIssuerID != issuer {
			return
		}
		row := sums[rec.CallerKeyID]
		if row == nil {
			row = &issuerSpend{KeyID: rec.CallerKeyID}
			sums[rec.CallerKeyID] = row
		}
		row.Name = rec.CallerKeyName
		if !rec.HandedOn {
			row.Calls++
			out.Total.Calls++
		}
		row.Input += rec.Input
		row.Output += rec.Output
		out.Total.Input += rec.Input
		out.Total.Output += rec.Output
		if c, ok := cost(rec); ok {
			row.Cost += c
			out.Total.Cost += c
		}
	})
	// the name magpie has now for a key it still lists, else the one the
	// record kept, else the id: a key renamed since reads as it is now
	names := map[string]string{}
	if keys, err := access.List(); err == nil {
		for _, k := range keys {
			names[k.ID] = k.Name
		}
	}
	for _, row := range sums {
		row.Name = firstWord(names[row.KeyID], row.Name, row.KeyID)
		out.Keys = append(out.Keys, *row)
	}
	// the keys that used most first, as every other list of usage is
	slices.SortFunc(out.Keys, func(a, b issuerSpend) int {
		if x, y := a.Input+a.Output, b.Input+b.Output; x != y {
			if x > y {
				return -1
			}
			return 1
		}
		if a.Calls != b.Calls {
			return b.Calls - a.Calls
		}
		return strings.Compare(a.KeyID, b.KeyID)
	})
	return out
}

// firstWord is the first of these that isn't empty.
func firstWord(words ...string) string {
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			return w
		}
	}
	return ""
}
