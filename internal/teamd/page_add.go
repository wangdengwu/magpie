package teamd

import "html/template"

// The add flow's two pages. They take a small view of their own rather than
// the Accounts page's data: nothing on them needs another team's anything,
// and a template handed less can't print more.

// addView is /team/<token>/add.
type addView struct {
	Team   Team
	Group  Group
	Token  string
	Notice addNotice
}

// signInView is /team/<token>/signin/<id>.
type signInView struct {
	Team   Team
	Token  string
	SignIn SignIn
	Notice addNotice
}

func addPage(g Grant, token string, notice addNotice) string {
	return render(template.Must(template.New("add").Parse(addHTML)), addView{
		Team: g.Team(), Group: g.Group(), Token: token, Notice: notice,
	})
}

func signInPage(g Grant, token string, st SignIn, notice addNotice) string {
	return render(template.Must(template.New("signin").Parse(signInHTML)), signInView{
		Team: g.Team(), Token: token, SignIn: st, Notice: notice,
	})
}
