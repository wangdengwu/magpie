package teamd

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// The usage page: what the team is spending.
//
// Two things are read, and they are not the same kind of number:
//
//   - each account's allowance, from magpie's own reading of it (the
//     vendor's windows), which is what tells a lead whose account is being
//     used up;
//   - the team's gateway key's budget, from magpie's count of what the key
//     spent, which is what the team's members actually reach.
//
// Both are readings taken at a time, and the page says when. Magpie keeps a
// reading between requests and hands back the kept one when the vendor
// couldn't be asked; that is not the same as a fresh one, and the page says
// which it has rather than showing a number that looks live.

// Share is how much of the account's narrowest window is used, 0 to 100;
// None when no window gives a share — the account was never read, or its
// windows are words rather than percentages.
type UsageAccount struct {
	Entry   string
	Name    string
	Plan    string
	On      bool
	Reading *Reading
	Note    string
	Share   float64
	None    bool
	// Lost is an entry the team's group holds that names no account magpie
	// has, and Key one of a provider's API keys: neither has a spend of its
	// own to show, and each says so in its own words rather than as an
	// account nobody has read.
	Lost bool
	Key  bool
}

// Spent says the account's tightest window is used up: it will be the one
// that stops it.
func (a UsageAccount) Spent() bool { return !a.None && a.Share >= 100 }

// ShareInt is Share as a whole percent, for the chip the page draws.
func (a UsageAccount) ShareInt() int { return shareInt(a.Share, 100) }

// UsageLimit is one of the team key's budgets and what it has spent of it.
type UsageLimit struct {
	// What the limit holds the key to: tokens, cost, or both.
	Tokens     int64
	Cost       float64
	TokensUsed int64
	CostUsed   float64
	// TokensLeft and CostLeft are magpie's own arithmetic, not this page's:
	// a key's budget is worked out against the window magpie has, and
	// recomputing it here would say a different number when the two
	// disagree.
	TokensLeft int64
	CostLeft   float64
	Period     string
	Days       int
	Hours      int
	// Start and Reset are the window magpie worked out when it answered.
	// ResetUnknown says it couldn't: an account limit whose account hasn't
	// been read. Reset is then zero, which is not "resets now".
	Start        time.Time
	Reset        time.Time
	ResetUnknown bool
	// Spent says the key is refused until Reset.
	Spent bool
	// Calls, Unpriced and CacheReads say what was counted and what wasn't.
	Calls      int
	Unpriced   int
	CacheReads bool
	// Models and Account scope the limit, "" for the whole key.
	Models  []string
	Account string
	Window  string
}

// Usage is the usage page's data: the team's accounts and the team's key.
type Usage struct {
	Team  Team
	Group Group
	Token string
	// Accounts are the team's group's accounts, those spending first.
	Accounts []UsageAccount
	// Key is the team's gateway key as magpie lists it, nil when magpie has
	// no key by the id teams.json names. KeyErr is why it couldn't be read.
	Key    *GatewayKey
	KeyErr string
	// Limits are the key's budgets and what it spent of each, in magpie's
	// order; a key with none has no cap.
	Limits []UsageLimit
	// UsageReadAt is when the newest account reading was taken, nil when
	// none was; KeyReadAt is when the key's budget was read, which is this
	// request. Neither is ever presented as live when it isn't.
	UsageReadAt *time.Time
	KeyReadAt   time.Time
	// Now is the clock the ages are counted from.
	Now time.Time
	// AccountsErr is why the accounts' readings couldn't be read, "" when
	// they were. The key's budget is still shown: one failing read does not
	// hide the other.
	AccountsErr string
	// Trans is the language the page is drawn in.
	Trans trans
	// Members are the keys the team issued, and what each spent over the
	// three periods; MemberErr is why they couldn't be read, "" when they
	// were. A failed read is said, never drawn as zeros.
	Members   []MemberUse
	MemberErr string
	// MemberTotals are all of them together, which is the same figure the
	// team key's own spend carries — a lead comparing the two is asking
	// whether the members are the team's whole spend.
	MemberTotals MemberTotals
	// MemberPeriods are the three columns, which the page draws from here
	// rather than naming them in its markup: the periods and their words
	// are one list.
	MemberPeriods []memberPeriod
}

// MemberUse is one member key's spend: the three periods the page shows side
// by side, today, the last 7 days and the last 30.
type MemberUse struct {
	ID   string
	Name string
	// Revoked says the team's live list no longer holds the key: it was
	// revoked or ran out, and its figures are what it used while it worked.
	Revoked bool
	Today   SpendTotals
	Week    SpendTotals
	Month   SpendTotals
}

// SpendTotals is what a member key used in one period.
type SpendTotals struct {
	Calls  int
	Input  int
	Output int
	Cost   float64
	// Read is false for a period whose read failed, so the cell says it
	// couldn't be read rather than showing a zero.
	Read bool
}

// Tokens is the period's tokens in and out.
func (s SpendTotals) Tokens() int { return s.Input + s.Output }

// Used says the key did anything at all in the period.
func (s SpendTotals) Used() bool { return s.Calls > 0 || s.Tokens() > 0 }

// MemberTotals is every member key's spend added up, over the three periods.
type MemberTotals struct {
	Today SpendTotals
	Week  SpendTotals
	Month SpendTotals
}

// memberPeriod is one of the three windows the page shows: magpie's period,
// and the word key its column's head is said with.
type memberPeriod struct {
	Period string
	Key    string
}

// memberPeriods are the three, in the order the page draws them.
var memberPeriods = []memberPeriod{
	{"today", "usage.member.today"},
	{"7d", "usage.member.week"},
	{"30d", "usage.member.month"},
}

// UsageFor is the page's data, from the team's own grant.
//
// Only this team's accounts are read out of magpie's answers: the readings
// are magpie's for every account of the agent, and what reaches the page is
// the group's, joined as the pool page joins them.
func (s *Server) UsageFor(ctx context.Context, g Grant, t trans) (Usage, error) {
	page := Usage{Trans: t, Team: g.Team(), Group: g.Group(), Now: s.now()}
	keys, err := s.admin.GatewayKeys(ctx)
	if err != nil {
		return Usage{}, err
	}
	page.KeyReadAt = s.now()
	for i := range keys {
		if strings.EqualFold(keys[i].ID, g.Team().KeyID) {
			page.Key = &keys[i]
			break
		}
	}
	if page.Key == nil {
		page.KeyErr = t.T("usage.err.key", argsOf{"ID": g.Team().KeyID})
	} else {
		uses := page.Key.Uses()
		for i, l := range page.Key.Limits() {
			row := UsageLimit{
				Tokens: l.Tokens, Cost: l.Cost,
				Period: l.Period, Days: l.Days, Hours: l.Hours,
				Models: l.Models, Account: l.Account, Window: l.AccountWindow,
				CacheReads: l.CacheReads,
			}
			if i < len(uses) {
				u := uses[i]
				row.TokensUsed, row.CostUsed = u.Tokens, u.Cost
				row.TokensLeft, row.CostLeft = u.TokensLeft, u.CostLeft
				row.Start, row.Reset, row.ResetUnknown = u.Start, u.Reset, u.ResetUnknown
				row.Spent, row.Calls, row.Unpriced = u.Spent, u.Calls, u.Unpriced
			}
			page.Limits = append(page.Limits, row)
		}
	}

	catalog, err := s.admin.Catalog(ctx)
	if err != nil {
		return Usage{}, err
	}
	usage, usageErr := s.admin.Usage(ctx, agentClaude)
	if usageErr != nil {
		// the accounts are still listed: which ones the team has is a
		// different question from what they have spent
		page.AccountsErr = t.T("usage.err.accounts")
	}
	for _, entry := range g.Accounts() {
		row := UsageAccount{Entry: entry}
		acct, known := catalog.Accounts[strings.ToLower(entry)]
		switch {
		case !known:
			row.Lost = true
			row.Note = t.T("usage.note.lost")
		case acct.Key:
			row.Key = true
			row.Name, row.Plan = acct.Name, acct.Plan
			row.Note = t.T("usage.note.key")
		default:
			row.Name, row.Plan = cmp.Or(acct.Name, acct.User), acct.Plan
			row.On = acct.On
			switch {
			case usageErr != nil:
				row.Note = page.AccountsErr
			default:
				r, ok := usage[accountKey(acct.User)]
				if !ok {
					row.Note = t.T("usage.note.no_reading")
					break
				}
				row.Reading = &r
				row.Share, row.None = narrowest(r)
				if r.ReadAt != nil && (page.UsageReadAt == nil || r.ReadAt.After(*page.UsageReadAt)) {
					page.UsageReadAt = r.ReadAt
				}
				row.Note = readingNote(t, r)
				if !row.On {
					row.Note = cmp.Or(row.Note, t.T("usage.note.not_serving"))
				}
			}
		}
		page.Accounts = append(page.Accounts, row)
	}
	// what is being spent first, then the accounts with no reading, then
	// the ones magpie isn't serving from: a lead is looking for the account
	// using the most
	sort.SliceStable(page.Accounts, func(i, j int) bool {
		a, b := page.Accounts[i], page.Accounts[j]
		if (a.Reading != nil) != (b.Reading != nil) {
			return a.Reading != nil
		}
		if a.Reading != nil && a.Share != b.Share {
			return a.Share > b.Share
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	page.Members, page.MemberTotals, page.MemberErr = s.memberUsage(ctx, t, g)
	return page, nil
}

// memberUsage reads what each of the team's member keys spent over the three
// periods, from magpie's ledger by the team's own issuer.
//
// The issuer is the team record's, never a request's: this is the same
// boundary the rest of the page keeps, and a page can't ask about another
// team's keys. A key the live list no longer holds is marked revoked and
// keeps its figures; one magpie's list holds is live.
//
// One period failing does not blank the others: each column says it couldn't
// be read, and the block says which. A read that fails is unknown, never
// zero.
func (s *Server) memberUsage(ctx context.Context, t trans, g Grant) ([]MemberUse, MemberTotals, string) {
	issued, err := s.admin.MemberKeys(ctx, g.Team().KeyID)
	if err != nil {
		return nil, MemberTotals{}, t.T("usage.member.keys_err")
	}
	if len(issued) == 0 {
		return nil, MemberTotals{}, ""
	}
	type periodAnswer struct {
		usage IssuerUsage
		err   error
	}
	answers := make([]periodAnswer, len(memberPeriods))
	var wg sync.WaitGroup
	for i, p := range memberPeriods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i].usage, answers[i].err = s.admin.IssuerUsage(ctx, g.Team().KeyID, p.Period)
		}()
	}
	wg.Wait()

	rows := map[string]*MemberUse{}
	row := func(id string) *MemberUse {
		r := rows[id]
		if r == nil {
			r = &MemberUse{ID: id}
			rows[id] = r
		}
		return r
	}
	var totals MemberTotals
	failed := ""
	for i, p := range memberPeriods {
		a := answers[i]
		if a.err != nil {
			if failed == "" {
				failed = t.T(p.Key)
			} else {
				failed += ", " + t.T(p.Key)
			}
			continue
		}
		spend := SpendTotals{Calls: a.usage.Total.Calls, Input: a.usage.Total.Input, Output: a.usage.Total.Output, Cost: a.usage.Total.Cost, Read: true}
		switch p.Period {
		case "today":
			totals.Today = spend
		case "7d":
			totals.Week = spend
		case "30d":
			totals.Month = spend
		}
		for _, k := range a.usage.Keys {
			r := row(k.KeyID)
			if r.Name == "" {
				r.Name = k.Name
			}
			spend := SpendTotals{Calls: k.Calls, Input: k.Input, Output: k.Output, Cost: k.Cost, Read: true}
			switch p.Period {
			case "today":
				r.Today = spend
			case "7d":
				r.Week = spend
			case "30d":
				r.Month = spend
			}
		}
	}
	live := map[string]bool{}
	for _, k := range issued {
		live[k.ID] = true
		r := row(k.ID)
		if r.Name == "" {
			r.Name = k.Name
		}
	}
	for id, r := range rows {
		r.Revoked = !live[id]
		if r.Name == "" {
			r.Name = id
		}
	}
	out := make([]MemberUse, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	// what used the most over the week first, then by name: a lead is
	// looking for the member spending the most
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].Week.Tokens(), out[j].Week.Tokens(); a != b {
			return a > b
		}
		if a, b := out[i].Week.Calls, out[j].Week.Calls; a != b {
			return a > b
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	why := ""
	if failed != "" {
		why = t.T("usage.member.err", argsOf{"Periods": failed})
	}
	return out, totals, why
}

// narrowest is the share of the account's most-used window: the one that
// will stop it first. None is set when no window gives a percentage — a
// reading that is words, a balance, or an account whose windows are empty —
// because 0 would read as "nothing used".
func narrowest(r Reading) (share float64, none bool) {
	none = true
	for _, w := range r.Windows {
		if w.Text || w.Unlimited || w.Lane != "" {
			continue
		}
		if none || w.Used > share {
			share, none = w.Used, false
		}
	}
	return share, none
}

// PeriodWords is a limit's window in the page's words: "per day", "every 5
// hours", "10 days of account".
func (l UsageLimit) PeriodWords(t trans) string {
	switch l.Period {
	case "days":
		return t.T("usage.period.days", argsOf{"N": l.Days})
	case "hours":
		return t.T("usage.period.hours", argsOf{"N": l.Hours})
	case "day":
		return t.T("usage.period.day")
	case "week":
		return t.T("usage.period.week")
	case "month":
		return t.T("usage.period.month")
	case "total":
		return t.T("usage.period.total")
	case "account":
		if l.Account != "" && l.Window != "" {
			return t.T("usage.period.account", argsOf{"Window": l.Window, "Account": l.Account})
		}
		return t.T("usage.period.an_account")
	}
	if l.Period == "" {
		return t.T("usage.period.unnamed")
	}
	return l.Period
}

// HasTokens and HasCost say which caps the limit holds the key to: a limit
// can have one, the other, or both.
func (l UsageLimit) HasTokens() bool { return l.Tokens > 0 }
func (l UsageLimit) HasCost() bool   { return l.Cost > 0 }

// TokenShare and CostShare are how much of each cap is used, a whole
// percent, never over 100 nor under 0 whatever the arithmetic says.
func (l UsageLimit) TokenShare() int { return shareInt(float64(l.TokensUsed), float64(l.Tokens)) }
func (l UsageLimit) CostShare() int  { return shareInt(l.CostUsed, l.Cost) }

// shareInt is used of limit as a whole percent, 0 when the limit is 0.
func shareInt(used, limit float64) int {
	if limit <= 0 {
		return 0
	}
	switch p := used / limit * 100; {
	case p <= 0:
		return 0
	case p >= 100:
		return 100
	default:
		return int(p + 0.5)
	}
}

// Resets is when the limit starts again, in the page's words, and Empty
// says the key is refused until then.
func (l UsageLimit) Resets(t trans, now time.Time) string {
	if l.ResetUnknown || l.Reset.IsZero() {
		return t.T("usage.reset_unknown")
	}
	return l.Reset.Local().Format("01-02 15:04") + " · " + until(t, now, l.Reset)
}

// serveUsage answers GET /team/{token}/usage.
func (s *Server) serveUsage(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t := transOf(r)
	page, err := s.UsageFor(ctx, g, t)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	page.Token = r.PathValue("token")
	s.render(w, t, http.StatusOK, usagePage(page))
}

// usageRoutes are the usage page's routes: the page a lead reads, and the
// same data as JSON for a script.
func (s *Server) usageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /team/{token}/usage", s.serveUsage)
	mux.HandleFunc("GET /team/{token}/usage.json", s.serveUsageJSON)
}

// serveUsageJSON is the page's data as JSON, filtered exactly as the page
// is: it is built from the same UsageFor and the same grant, so there is no
// second path to another team's accounts.
func (s *Server) serveUsageJSON(w http.ResponseWriter, r *http.Request) {
	g, ok := s.authorizePage(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t := transOf(r)
	page, err := s.UsageFor(ctx, g, t)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "magpie couldn't be read: nothing here is a real figure",
		})
		return
	}
	writeJSON(w, http.StatusOK, page.JSON())
}

// usageJSON is the page's data as the brief asks for it: the accounts and
// the key's limits, already filtered to this team.
//
// It is served beside the page rather than instead of it, for the same
// reason the page exists: a lead reading the pool may want to script it,
// and what a script may have is exactly what the page may have.
type usageJSON struct {
	Team    string         `json:"team"`
	Group   string         `json:"group"`
	Key     string         `json:"keyId"`
	ReadAt  *time.Time     `json:"usageReadAt,omitempty"`
	KeyRead time.Time      `json:"keyReadAt"`
	Error   string         `json:"accountsError,omitempty"`
	Rows    []usageJSONRow `json:"accounts"`
	Limits  []usageJSONLim `json:"limits"`
}

type usageJSONRow struct {
	Entry   string   `json:"entry"`
	Name    string   `json:"name,omitempty"`
	Plan    string   `json:"plan,omitempty"`
	On      bool     `json:"on"`
	Reading *Reading `json:"reading,omitempty"`
	Note    string   `json:"note,omitempty"`
}

type usageJSONLim struct {
	Period string `json:"period"`
	Days   int    `json:"days,omitempty"`
	Hours  int    `json:"hours,omitempty"`
	// Tokens, Cost and what's left of each, as magpie worked them out.
	Tokens     int64     `json:"tokens,omitempty"`
	Cost       float64   `json:"cost,omitempty"`
	TokensUsed int64     `json:"tokensUsed"`
	CostUsed   float64   `json:"costUsed"`
	TokensLeft int64     `json:"tokensLeft"`
	CostLeft   float64   `json:"costLeft"`
	Start      time.Time `json:"start"`
	Reset      time.Time `json:"reset,omitzero"`
	NoReset    bool      `json:"resetUnknown,omitempty"`
	Spent      bool      `json:"spent"`
	Calls      int       `json:"calls"`
	Unpriced   int       `json:"unpriced,omitempty"`
	Models     []string  `json:"models,omitempty"`
	Account    string    `json:"account,omitempty"`
	Window     string    `json:"accountWindow,omitempty"`
}

// JSON is the page's data in the shape the brief names.
func (u Usage) JSON() usageJSON {
	out := usageJSON{
		Team: u.Team.ID, Group: u.Group.ID, Key: u.Team.KeyID,
		ReadAt: u.UsageReadAt, KeyRead: u.KeyReadAt, Error: u.AccountsErr,
		Rows:   make([]usageJSONRow, 0, len(u.Accounts)),
		Limits: make([]usageJSONLim, 0, len(u.Limits)),
	}
	for _, a := range u.Accounts {
		out.Rows = append(out.Rows, usageJSONRow{
			Entry: a.Entry, Name: a.Name, Plan: a.Plan, On: a.On, Reading: a.Reading, Note: a.Note,
		})
	}
	for _, l := range u.Limits {
		out.Limits = append(out.Limits, usageJSONLim{
			Period: l.Period, Days: l.Days, Hours: l.Hours,
			Tokens: l.Tokens, Cost: l.Cost,
			TokensUsed: l.TokensUsed, CostUsed: l.CostUsed,
			TokensLeft: l.TokensLeft, CostLeft: l.CostLeft,
			Start: l.Start, Reset: l.Reset, NoReset: l.ResetUnknown, Spent: l.Spent,
			Calls: l.Calls, Unpriced: l.Unpriced,
			Models: l.Models, Account: l.Account, Window: l.Window,
		})
	}
	return out
}

// fmtTokens is a count in the page's words: 1.2M, 340k, 12.
//
// A count that rounds up to the next unit is said in it: 999,999 is 1M and
// not 1000k, and a billion is 1.5B rather than 1500M. A lead reading a cap
// compares these at a glance, and "1000k / 1M" reads as two units for one
// number.
func fmtTokens(n int64) string {
	if n <= 0 {
		return "0"
	}
	switch {
	case n < 1000:
		return itoa(int(n))
	case n < 999_950:
		return trimZero(float64(n)/1000) + "k"
	case n < 999_950_000:
		return trimZero(float64(n)/1_000_000) + "M"
	default:
		return trimZero(float64(n)/1_000_000_000) + "B"
	}
}

// trimZero is a number to one decimal place, with a trailing ".0" dropped.
func trimZero(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// fmtCost is money in the page's words: $1.20, $0.03, $0.
func fmtCost(f float64) string {
	if f <= 0 {
		return "$0"
	}
	return fmt.Sprintf("$%.2f", f)
}
