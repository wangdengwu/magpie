package teamd

import "html/template"

// The connect page's view. It is the page a lead sends to members, so it
// carries the one secret the portal ever prints — the team's key, when ops
// mounted it — and nothing else of anyone's.

func connectPage(c Connect) string {
	return render(template.Must(template.New("connect").Funcs(connectFuncs).Parse(connectHTML)), c)
}

// connectFuncs are what the page's template uses besides its values.
var connectFuncs = template.FuncMap{
	"env":      snippetEnv,
	"json":     snippetJSON,
	"check":    snippetCheck,
	"expected": expectedAnswer,
	"model":    checkModel,
	"plural":   plural,
}
