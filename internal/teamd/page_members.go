package teamd

import "html/template"

// The members page's view and markup: the team's member keys, and the form
// that issues one.

func membersPage(m Members) string {
	return render(template.Must(template.New("members").Funcs(m.Trans.funcs()).Parse(membersHTML)), m)
}

var membersHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "members.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel">{{.Team.Name}}</h1>
  <h2>{{T "members.head"}}</h2>
</div>
<div class="sum">
  <span>{{Tn "members.keys" (len .Rows) (args "N" (len .Rows))}}</span>
  <span>{{T "members.issued" (args "Issuer" .Issuer)}}</span>
  <span class="grow"><a class="btn" href="/team/{{.Token}}">{{T "members.nav.accounts"}}</a></span>
  <a class="btn" href="/team/{{.Token}}/usage">{{T "accounts.nav.usage"}}</a>
  <a class="btn" href="/team/{{.Token}}/connect">{{T "accounts.nav.connect"}}</a>
  <a class="btn" href="/team/{{.Token}}/audit">{{T "accounts.nav.audit"}}</a>
</div>

{{- if .Issued}}
<div class="card prose">
  <div class="top">
    <div class="who">
      <div class="name">{{T "members.issued.title" (args "Name" .Issued.Name)}}</div>
      <div class="meta plain">{{T "members.issued.why"}}</div>
    </div>
  </div>
  <p class="link mono sel">{{.Issued.Key}}</p>
  <p class="note">{{.IssuedUntil}}</p>
</div>
{{- end}}

{{- if .Notice.Text}}
<p class="note{{if .Notice.Bad}} lost{{else}} ok{{end}}">{{.Notice.Text}}</p>
{{- end}}

{{- if .CanIssue}}
<div class="card prose">
  <div class="top">
    <div class="who">
      <div class="name">{{T "members.form.title"}}</div>
      <div class="meta plain">{{T "members.form.why"}}</div>
    </div>
  </div>
  <form method="post" action="/team/{{.Token}}/members">
    <input class="field" type="text" name="name" autocomplete="off" autocapitalize="off"
           autocorrect="off" spellcheck="false" placeholder="{{T "members.form.placeholder"}}" maxlength="120" required>
    <select class="field" name="ttl">
      {{- range .TTLs}}
      <option value="{{.Value}}">{{T .Key}}</option>
      {{- end}}
    </select>
    <button class="btn" type="submit">{{T "members.form.button"}}</button>
  </form>
  <p class="note">{{T "members.form.note"}}</p>
</div>
{{- else}}
<div class="card prose">
  <div class="name">{{T "members.cant"}}</div>
  <p class="note warn">{{.Why}}</p>
</div>
{{- end}}

<h2 class="sect">{{T "members.list.head"}}</h2>
{{- if .Rows}}
<div class="cards">
  {{- range .Rows}}
  <div class="card{{if .Expired}} off{{end}}">
    <div class="top">
      <div class="who">
        <div class="name">{{.Name}}</div>
        <div class="meta"><span class="mono">{{.ID}}</span>{{if .Expired}} · {{T "members.row.expired"}}{{else if .Permanent}} · {{T "members.row.permanent"}}{{else}} · {{T "members.row.until" (args "At" .When)}}{{end}}</div>
      </div>
      <div class="act">
        <form method="post" action="/team/{{$.Token}}/members/{{.ID}}/revoke">
          <button class="btn small danger" type="submit">{{T "members.row.revoke"}}</button>
        </form>
      </div>
    </div>
  </div>
  {{- end}}
</div>
{{- else}}
<p class="note">{{T "members.none"}}</p>
{{- end}}

</div></body></html>
`
