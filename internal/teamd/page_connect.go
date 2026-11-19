package teamd

import "html/template"

// The connect page's view. It is the page a lead sends to members, so it
// carries the one secret the portal ever prints — the team's key, when ops
// mounted it — and nothing else of anyone's.

func connectPage(c Connect) string {
	return render(template.Must(template.New("connect").Funcs(connectFuncs(c.Trans)).Parse(connectHTML)), c)
}

// connectFuncs are what the page's template uses besides its values, and
// the language it says its words in.
func connectFuncs(t trans) template.FuncMap {
	funcs := pageFuncs(t)
	funcs["env"] = snippetEnv
	funcs["json"] = snippetJSON
	funcs["check"] = snippetCheck
	funcs["expected"] = expectedAnswer
	funcs["model"] = checkModel
	funcs["modelsMore"] = func(t trans, partial bool) string {
		if !partial {
			return ""
		}
		return t.T("connect.models.more")
	}
	return funcs
}
