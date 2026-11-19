package teamd

import "html/template"

// The members page's view and markup: the team's member keys, and the form
// that issues one.

func membersPage(m Members) string {
	return render(template.Must(template.New("members").Funcs(template.FuncMap{"plural": plural}).Parse(membersHTML)), m)
}

var membersHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Team.Name}} · Member keys</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel">{{.Team.Name}}</h1>
  <h2>Member keys</h2>
</div>
<div class="sum">
  <span>{{len .Rows}} {{plural (len .Rows) "key" "keys"}}</span>
  <span>issued under <span class="mono sel">{{.Issuer}}</span></span>
  <span class="grow"><a class="btn" href="/team/{{.Token}}">Accounts</a></span>
  <a class="btn" href="/team/{{.Token}}/usage">Usage</a>
  <a class="btn" href="/team/{{.Token}}/connect">Connect</a>
  <a class="btn" href="/team/{{.Token}}/audit">History</a>
</div>

{{- if .Issued}}
<div class="card prose">
  <div class="top">
    <div class="who">
      <div class="name">The key for {{.Issued.Name}}</div>
      <div class="meta plain">This is the only time it is shown. Copy it now; the list below never shows it again.</div>
    </div>
  </div>
  <p class="link mono sel">{{.Issued.Key}}</p>
  <p class="note">It works until {{.IssuedUntil}}. Give it to the member and nobody else.</p>
</div>
{{- end}}

{{- if .Notice.Text}}
<p class="note{{if .Notice.Bad}} lost{{else}} ok{{end}}">{{.Notice.Text}}</p>
{{- end}}

{{- if .CanIssue}}
<div class="card prose">
  <div class="top">
    <div class="who">
      <div class="name">Issue a key to a member</div>
      <div class="meta plain">Each key counts against this team's own limits, and stops the moment this team's key is switched off.</div>
    </div>
  </div>
  <form method="post" action="/team/{{.Token}}/members">
    <input class="field" type="text" name="name" autocomplete="off" autocapitalize="off"
           autocorrect="off" spellcheck="false" placeholder="The member's name" maxlength="120" required>
    <select class="field" name="ttl">
      {{- range .TTLs}}
      <option value="{{.Value}}">{{.Word}}</option>
      {{- end}}
    </select>
    <button class="btn" type="submit">Issue the key</button>
  </form>
  <p class="note">A name can't be used twice while the key it named still works. Revoke a key to free its name.</p>
</div>
{{- else}}
<div class="card prose">
  <div class="name">This team can't issue keys yet</div>
  <p class="note warn">{{.Why}}</p>
</div>
{{- end}}

<h2 class="sect">The team's keys</h2>
{{- if .Rows}}
<div class="cards">
  {{- range .Rows}}
  <div class="card{{if .Expired}} off{{end}}">
    <div class="top">
      <div class="who">
        <div class="name">{{.Name}}</div>
        <div class="meta"><span class="mono">{{.ID}}</span>{{if .Expired}} · expired{{else if .Permanent}} · permanent{{else}} · until {{.When}}{{end}}</div>
      </div>
      <div class="act">
        <form method="post" action="/team/{{$.Token}}/members/{{.ID}}/revoke">
          <button class="btn small danger" type="submit">Revoke</button>
        </form>
      </div>
    </div>
  </div>
  {{- end}}
</div>
{{- else}}
<p class="note">No keys yet. Issue one above, and give it to the member it is named for.</p>
{{- end}}

</div></body></html>
`
