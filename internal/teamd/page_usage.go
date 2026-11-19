package teamd

import (
	"html/template"
	"time"
)

// The usage page's view: the same data the JSON carries, plus what only a
// page needs — the words for a limit's window and the times it resets.
//
// It takes the Usage struct rather than a view of its own: every figure on
// it is one the JSON has too, and a second shape is a second thing to keep
// in step. Nothing here is added to what UsageFor read from the team's own
// grant.

func usagePage(u Usage) string {
	u.MemberPeriods = memberPeriods
	return render(template.Must(template.New("usage").Funcs(usageFuncs(u.Trans)).Parse(usageHTML)), u)
}

// usageFuncs are what the page's template uses besides its values, and the
// language it says its words in.
func usageFuncs(t trans) template.FuncMap {
	funcs := pageFuncs(t)
	funcs["tokens"] = fmtTokens
	funcs["cost"] = fmtCost
	funcs["limitWord"] = func(l UsageLimit) string { return l.PeriodWords(t) }
	funcs["memberCell"] = memberCell
	funcs["resets"] = func(l UsageLimit, now time.Time) string { return l.Resets(t, now) }
	funcs["membersURL"] = func(token string) string { return "/team/" + token + "/members" }
	return funcs
}
