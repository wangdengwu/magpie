package teamd

import "html/template"

// The add flow's two pages. They take a small view of their own rather than
// the Accounts page's data: nothing on them needs another team's anything,
// and a template handed less can't print more.

// addView is /team/<token>/add.
type addView struct {
	Trans  trans
	Team   Team
	Group  Group
	Token  string
	Notice addNotice
}

// signInView is /team/<token>/signin/<id>.
type signInView struct {
	Trans  trans
	Team   Team
	Token  string
	SignIn SignIn
	Notice addNotice
}

// signInPlan is the plan as a sentence says it, " on Max" or nothing.
func signInPlan(st SignIn) string {
	if st.Plan == "" {
		return ""
	}
	return " on " + st.Plan
}

// signInWords is the sign-in's state in the page's words.
func signInWords(t trans, st SignIn) string {
	switch st.State {
	case "waiting":
		return t.T("signin.waiting")
	case "done":
		return t.T("signin.done.state")
	case "failed":
		return t.T("signin.failed")
	case "canceled":
		return t.T("signin.canceled")
	}
	return st.State
}

func addPage(t trans, g Grant, token string, notice addNotice) string {
	funcs := t.funcs()
	funcs["signInWords"] = func(st SignIn) string { return signInWords(t, st) }
	funcs["signInPlan"] = signInPlan
	return render(template.Must(template.New("add").Funcs(funcs).Parse(addHTML)), addView{
		Trans: t, Team: g.Team(), Group: g.Group(), Token: token, Notice: notice,
	})
}

func signInPage(t trans, g Grant, token string, st SignIn, notice addNotice) string {
	funcs := t.funcs()
	funcs["signInWords"] = func(st SignIn) string { return signInWords(t, st) }
	funcs["signInPlan"] = signInPlan
	return render(template.Must(template.New("signin").Funcs(funcs).Parse(signInHTML)), signInView{
		Trans: t, Team: g.Team(), Token: token, SignIn: st, Notice: notice,
	})
}
