package teamd

import "html/template"

// The usage page's view: the same data the JSON carries, plus what only a
// page needs — the words for a limit's window and the times it resets.
//
// It takes the Usage struct rather than a view of its own: every figure on
// it is one the JSON has too, and a second shape is a second thing to keep
// in step. Nothing here is added to what UsageFor read from the team's own
// grant.

func usagePage(u Usage) string {
	return render(template.Must(template.New("usage").Funcs(usageFuncs).Parse(usageHTML)), u)
}

// usageFuncs are what the page's template uses besides its values.
var usageFuncs = template.FuncMap{
	"ago":       ago,
	"until":     until,
	"windows":   windows,
	"plural":    plural,
	"tokens":    fmtTokens,
	"cost":      fmtCost,
	"limitWord": limitWord,
}

// limitWord is a limit's window in the page's words: "every 5 hours",
// "per week", "5h of claude/alice@example.com".
func limitWord(l UsageLimit) string { return l.PeriodWords() }
