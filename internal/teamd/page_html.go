package teamd

// The portal's pages, as constants. They are parsed at the point of use
// from these strings, so a page can't fail to load because a file was left
// out of an image, and every value goes through html/template's escaping.
//
// The look is magpie's own, taken from internal/gui/assets/app.css: the
// same colour tokens (--bg, --card, --line, --fg, --muted, --accent,
// --green/--amber/--red, and their -soft grounds), the same type scale, and
// the same shape for a subscription's card — a head, the windows as a
// two-column grid over 4px bars, and a reset line under each. A team's lead
// moves between magpie's page and this one all day, and the two should not
// read as two products.
//
// What differs is only what a page needs and an app window doesn't: the
// text is selectable, the layout has a reading width, and nothing loads
// from anywhere — no image, no font, no script. The token is in the
// address bar, so a page that called out would hand it over in a Referer.
//
// What a page may say about accounts is a separate promise, kept by what
// it is handed (page.go): only this team's group's accounts are ever in it.

// pageStyle is magpie's palette and components, at the scale a page reads
// at. Every colour is app.css's, value for value, so light and dark match
// the app beside them.
const pageStyle = `
:root {
  --bg:#f4f4f6; --card:#fff; --card-2:#f9f9fb; --pill:#f1f1f4; --pill-hover:#e9e9ee;
  --line:#e3e3e8; --line-2:#ececf0;
  --fg:#1c1c21; --fg-2:#4b4b53; --muted:#85858d; --faint:#b9b9c2; --ctl-fg:#63636b;
  --accent:#4f46e5; --accent-soft:#eeedfd;
  --green:#189a58; --green-soft:#e4f6ec; --red:#d64545; --red-soft:#fbe9e9;
  --amber:#b45309; --amber-soft:#fbf0df;
  --font:-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI",system-ui,sans-serif;
  --code:ui-monospace,"SF Mono",Menlo,Consolas,monospace;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg:#1a1a1e; --card:#232327; --card-2:#1f1f23; --pill:#2d2d33; --pill-hover:#36363d;
    --line:#333339; --line-2:#2c2c32;
    --fg:#ededf1; --fg-2:#b9b9c1; --muted:#8b8b94; --faint:#56565f; --ctl-fg:#a1a1aa;
    --accent:#a5b4fc; --accent-soft:#2b2c47;
    --green:#6fd99b; --green-soft:#1e3a2b; --red:#f28b82; --red-soft:#45272a;
    --amber:#f2b544; --amber-soft:#3f3220;
  }
}
:root { color-scheme:light dark }
* { box-sizing:border-box; margin:0; padding:0 }
body {
  font:13px/1.4 var(--font); color:var(--fg); background:var(--bg);
  -webkit-font-smoothing:antialiased; font-variant-numeric:tabular-nums;
}
/* magpie's pages fill their window and lay cards across it; so does this
   one. The reading measure is kept on the text inside a card instead
   (.prose), not by narrowing the whole page. */
.wrap { max-width:78rem; margin:0 auto; padding:26px 22px 64px }
h1 { font-size:17px; font-weight:600; letter-spacing:-.01em }
h2 { font-size:12.5px; font-weight:600; color:var(--fg-2) }
code, .mono { font:11.5px var(--code) }
.sel { user-select:text; -webkit-user-select:text }

/* the page's head: the team, then what it holds */
.head { display:flex; align-items:baseline; gap:4px 10px; flex-wrap:wrap; margin-bottom:3px }
.sum { display:flex; align-items:center; gap:4px 14px; flex-wrap:wrap; font-size:11.5px; color:var(--muted); margin-bottom:16px }
/* no separators between these: one that wraps on its own leaves a dot at
   the end of a line with nothing after it, and one glued to the next item
   is read out as part of it ("·Claude pool"). The gap and the two colours
   already say where one ends and the next begins. */
.sum .mono { color:var(--fg-2) }

/* magpie's chip: whatever is set beside a name reads as one */
.chip {
  display:inline-flex; align-items:center; gap:4px; height:20px; padding:0 8px;
  border-radius:999px; border:1px solid var(--line); background:var(--pill);
  font-size:10.5px; color:var(--ctl-fg); white-space:nowrap;
}
.chip.on { border-color:transparent; background:var(--green-soft); color:var(--green) }
.chip.off { border-color:transparent; background:var(--pill); color:var(--muted) }
.chip.paused { border-color:transparent; background:var(--amber-soft); color:var(--amber) }
.chip.lost { border-color:transparent; background:var(--red-soft); color:var(--red) }
.chip.key { border-color:transparent; background:var(--accent-soft); color:var(--accent) }

/* an account: magpie's subscription card, one per account */
.cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(21rem,1fr)); gap:9px; align-items:start }
.card {
  padding:11px 13px 12px; border:1px solid var(--line); border-radius:10px;
  background:var(--card); min-width:0;
}
.card.off { background:var(--card-2) }
.card .top { display:flex; align-items:center; gap:12px; margin-bottom:9px }
.card .top .who { min-width:0; flex:1 1 auto }
.card .top .name { font-size:12.5px; font-weight:600; overflow:hidden; text-overflow:ellipsis; white-space:nowrap }
.card .top .meta { font-size:10.5px; color:var(--muted); overflow:hidden; text-overflow:ellipsis; white-space:nowrap }
/* a plan's name is capitalized as magpie shows it; a sentence beside it is not */
.card .top .meta .plan { text-transform:capitalize }
.card .top .meta.plain { white-space:normal; font-size:11px }
.card .top .meta .mono { text-transform:none; color:var(--muted) }

/* the windows: magpie's two-column quota grid, a 4px bar under each */
.wins { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:9px 14px }
.win { display:grid; grid-template-columns:minmax(0,1fr); align-content:start }
.wins.one { grid-template-columns:minmax(0,1fr) }
.labels { display:flex; flex-wrap:wrap; justify-content:space-between; align-items:baseline; gap:2px 6px; margin-bottom:4px; font-size:10.5px; color:var(--muted) }
.labels .n { color:var(--fg-2); font-weight:600; white-space:nowrap }
.track { height:4px; border-radius:2px; background:var(--pill); overflow:hidden }
.track i { display:block; height:100%; border-radius:inherit; background:var(--accent) }
.track.full i { background:var(--amber) }
.reset { margin-top:4px; font-size:10px; color:var(--muted); display:flex; flex-wrap:wrap; column-gap:4px }

/* what a window can't say */
.note { margin-top:8px; font-size:10.5px; color:var(--muted); line-height:1.45 }
.note.warn { color:var(--amber) }
.note.ok { color:var(--green) }
.note.lost { color:var(--red) }
.read { margin-top:7px; font-size:10px; color:var(--muted) }

/* a pool with nothing in it */
.empty {
  padding:30px 20px; border:1px dashed var(--line); border-radius:10px;
  background:var(--card); text-align:center;
}
.empty .big { font-size:12.5px; font-weight:600 }
.empty p + p { margin-top:5px; font-size:11.5px; color:var(--muted) }

/* a whole page that is one message: magpie's error text, at page size */
.said { max-width:32rem; margin:12vh auto 0; padding:0 16px; text-align:center }
.said .big { font-size:14px; font-weight:600; margin-bottom:6px }
.said p { font-size:12px; color:var(--muted); line-height:1.5 }

/* a form's own controls, drawn as magpie draws a pill button */
a { color:var(--accent); text-decoration:none }
a:hover { text-decoration:underline }
a.back { color:var(--muted); margin-right:2px }
.grow { margin-left:auto }
.btn {
  display:inline-flex; align-items:center; height:26px; padding:0 12px;
  border-radius:7px; border:1px solid var(--line); background:var(--pill);
  color:var(--fg); font:inherit; font-size:12px; cursor:pointer; white-space:nowrap;
}
.btn:hover { background:var(--pill-hover); text-decoration:none }
.btn:focus-visible { outline:2px solid var(--accent); outline-offset:1px }
.btn.quiet { background:none; border-color:transparent; color:var(--muted); margin-top:10px }
.btn.quiet:hover { background:var(--pill) }
/* a control that changes one account, in its row's head */
.act { display:flex; align-items:center; gap:8px }
.btn.small { height:22px; padding:0 9px; font-size:11px }
.btn.danger { color:var(--red) }
.why { font-size:10.5px; color:var(--amber); line-height:1.45; margin-top:8px }
.why + .note { margin-top:5px }
.field {
  width:100%; height:28px; padding:0 9px; margin:8px 0;
  border:1px solid var(--line); border-radius:7px; background:var(--card); color:var(--fg);
  font:11.5px var(--code);
}
.field:focus-visible { outline:2px solid var(--accent); outline-offset:1px }
.step { font-size:12px; margin-bottom:8px }
.steps { margin:0 0 12px 18px; font-size:11.5px; color:var(--fg-2) }
.steps li { margin-bottom:5px }
.link { padding:8px 10px; border-radius:7px; background:var(--card-2); border:1px solid var(--line); overflow-wrap:anywhere; font-size:11px }

/* magpie's rows fill the window with their content at the left; so does a
   card here. A line of explanation stops where a line stops being read,
   which is the text's measure and not the card's. */
.prose > * { max-width:48rem }
.prose > * + * { margin-top:8px }

/* a section of a longer page: the pool's head, then what it is spending */
h2.sect { font-size:12.5px; font-weight:600; color:var(--fg-2); margin:22px 0 9px }
h2.sect:first-of-type { margin-top:0 }

/* something a reader copies: a shell command or a settings file.
   It scrolls sideways rather than wrapping. A wrapped JSON string is still
   valid JSON, but a wrapped value is not what a reader sees them copy:
   they copy the line, and half a key is a 401 nobody can see. The key is
   long enough to scroll a little; leaving it whole is worth that. */
.code {
  margin:8px 0; padding:9px 11px; border:1px solid var(--line); border-radius:7px;
  background:var(--card-2); overflow-x:auto;
}
.code pre {
  font:11.5px/1.55 var(--code); color:var(--fg); white-space:pre;
  user-select:text; -webkit-user-select:text;
}
.chips { display:flex; flex-wrap:wrap; gap:5px; margin-top:8px }

/* "if it doesn't work": a term and what to do about it */
.fails { font-size:11.5px; line-height:1.5 }
.fails dt { font-weight:600; color:var(--fg); margin-top:10px }
.fails dt:first-child { margin-top:0 }
.fails dd { color:var(--muted); margin:3px 0 0 0 }

/* a table of what happened: a row per change, a refused one marked */
.log { width:100%; border-collapse:collapse; font-size:11.5px }
.log th { text-align:left; font-weight:600; color:var(--muted); font-size:10.5px; padding:0 10px 6px 0; border-bottom:1px solid var(--line) }
.log th:last-child, .log td:last-child { padding-right:0 }
.log td.mark .chip { display:inline-flex }
.log td { padding:6px 10px 6px 0; border-bottom:1px solid var(--line-2); vertical-align:top }
.log tr:last-child td { border-bottom:none }
/* the members' block adds every member up under their rows */
.log tfoot td { border-top:1px solid var(--line); border-bottom:none; font-weight:600 }
.log .when { white-space:nowrap }
.log .muted { color:var(--muted) }
.log tr.refused td { color:var(--red) }
.log tr.refused .mono { color:var(--red) }
@media (max-width:34rem) {
  .log .when .muted { display:none }
  /* a table of five columns is not a table on a phone: each change becomes
     a block, its labels on the left of what they name. Splitting the row
     keeps every value on one line, where five columns at 390px left one
     word a line. */
  .log thead { display:none }
  .log, .log tbody, .log tr, .log td { display:block; width:auto }
  .log tr { padding:8px 0; border-bottom:1px solid var(--line-2) }
  .log tr:last-child { border-bottom:none }
  .log td { padding:1px 0; border:none; display:grid; grid-template-columns:4.2rem minmax(0,1fr); gap:6px }
  .log td::before { content:attr(data-label); color:var(--muted); font-size:10px }
  /* the member keys' periods are three of the same kind, and their labels
     are longer than a log's ("Last 30 days"): the column is wider, so a
     label sits on its value's line instead of over two */
  .log td[data-label="Last 7 days"], .log td[data-label="Last 30 days"],
  .log td[data-label="Today"] { grid-template-columns:5.6rem minmax(0,1fr) }
  /* the mark is not a labelled field: it sits under the rest, its size
     kept, and an empty one takes no room at all */
  .log td.mark { display:block }
  .log td.mark:empty { display:none }
  .log td.mark .chip { margin-top:4px }
}

@media (max-width:34rem) {
  .cards { grid-template-columns:minmax(0,1fr) }
  .wins { grid-template-columns:minmax(0,1fr) }
}
`

// accountsHTML is /team/<token>: the team's accounts, with their windows.
//
// Its sentences come from the word list (words.go), not from here: a page
// written this way is the same in every language, and a language that is
// missing one sentence falls back to English rather than to a blank.
var accountsHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "accounts.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel">{{.Team.Name}}</h1>
  <h2>{{T "accounts.head"}}</h2>
</div>
<div class="sum">
  <span>{{Tn "accounts.in_use" .Total (args "On" .On "Total" .Total "Accounts" .Total)}}</span>
  <span>{{T "accounts.group" (args "Name" .Group.Name "ID" .Group.ID)}}</span>
  {{- if .ReadAt}}
  <span>{{T "accounts.read" (args "Ago" (ago .Now .ReadAt))}}</span>
  {{- end}}
  <span class="grow"><a class="btn" href="/team/{{.Token}}/usage">{{T "accounts.nav.usage"}}</a></span>
  <a class="btn" href="/team/{{.Token}}/connect">{{T "accounts.nav.connect"}}</a>
  <a class="btn" href="/team/{{.Token}}/members">{{T "accounts.nav.members"}}</a>
  <a class="btn" href="/team/{{.Token}}/audit">{{T "accounts.nav.audit"}}</a>
  <a class="btn" href="/team/{{.Token}}/add">{{T "accounts.nav.add"}}</a>
</div>

{{- if .Added}}
<p class="note ok">{{T "accounts.added" (args "Entry" .Added)}}</p>
{{- end}}

{{- if .Notice.Text}}
<p class="note {{if .Notice.Bad}}lost{{else}}ok{{end}}">{{.Notice.Text}}</p>
{{- end}}

{{- if .More}}
<p class="note warn">{{T "accounts.more" (args "Shown" (len .Rows) "More" .More)}}</p>
{{- end}}

{{- if not .Rows}}
<div class="empty">
  <p class="big">{{T "accounts.empty"}}</p>
  <p>{{T "accounts.empty.why" (args "Name" .Group.Name)}}</p>
</div>
{{- else}}
<div class="cards">
{{- range .Rows}}
{{- $row := .}}
<section class="card{{if not .On}} off{{end}}">
  <div class="top">
    <div class="who">
      <div class="name sel">{{if .Name}}{{.Name}}{{else}}{{.Entry}}{{end}}</div>
      {{- if or .Known .ProviderName}}
      <div class="meta">{{.ProviderName}}{{if .Plan}} · <span class="plan">{{.Plan}}</span>{{end}}{{if .Known}} · <span class="mono sel">{{.Entry}}</span>{{end}}</div>
      {{- end}}
    </div>
    {{- if not .Known}}<span class="chip lost">{{T "accounts.chip.lost"}}</span>
    {{- else if .Key}}<span class="chip key">{{T "accounts.chip.key"}}</span>
    {{- else if .On}}<span class="chip on">{{T "accounts.chip.on"}}</span>
    {{- else if .Paused}}<span class="chip paused">{{T "accounts.chip.paused"}}</span>
    {{- else}}<span class="chip off">{{T "accounts.chip.off"}}</span>{{end}}
    {{- with .Switch $.Trans}}
    <form method="post" action="/team/{{$.Token}}/accounts/{{.Action}}">
      <input type="hidden" name="account" value="{{$row.Entry}}">
      <button class="btn small{{if eq .Action "off"}} danger{{end}}" type="submit">{{.Label}}</button>
    </form>
    {{- end}}
  </div>

  {{- with .Warning $.Trans}}
  <p class="why">{{.}}</p>
  {{- end}}
  {{- with .WhyNot $.Trans}}
  <p class="why">{{.}}</p>
  {{- end}}

  {{- if .Reading}}
    {{- $r := .Reading}}
    {{- $w := windows $r $.Now}}
    {{- if $w}}
    <div class="wins{{if eq (len $w) 1}} one{{end}}">
      {{- range $w}}
      <div class="win">
        <div class="labels">
          <span>{{.Name}}</span>
          {{- if .Text}}<span class="n sel">{{if .Display}}{{.Display}}{{else}}—{{end}}</span>
          {{- else}}<span class="n sel">{{.ShareInt}}%</span>{{end}}
        </div>
        {{- if not .Text}}
        <div class="track{{if .Empty}} full{{end}}"><i style="width:{{.ShareInt}}%"></i></div>
        {{- end}}
        {{- if .Resets}}
        <div class="reset"><span>{{T "accounts.resets" (args "At" .Resets)}}{{if .ResetsIn}} · {{.ResetsIn}}{{end}}</span></div>
        {{- end}}
      </div>
      {{- end}}
    </div>
    {{- end}}
    {{- if $r.ReadAt}}
    <div class="read">{{T "accounts.read.at" (args "Ago" (ago $.Now $r.ReadAt))}}</div>
    {{- end}}
  {{- end}}

  {{- if .Note}}
  <p class="note{{if not .Known}} lost{{end}}">{{.Note}}</p>
  {{- end}}
</section>
{{- end}}
</div>
{{- end}}

</div></body></html>
`

// notFoundHTML is what a page says when the link names no team. It says
// nothing about why: a chat client that cut a token short must not learn
// which part was wrong, and neither must a guess.
var notFoundHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "notfound.title"}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="said">
<p class="big">{{T "notfound.big"}}</p>
<p>{{T "notfound.why"}}</p>
</div></body></html>
`

// errorHTML is what a page says when magpie couldn't be read.
var errorHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "error.title"}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="said">
<p class="big">{{T "error.big"}}</p>
<p>{{T "error.why"}}</p>
</div></body></html>
`

// addHTML is /team/<token>/add: what adding an account will ask for, and the
// button that starts one. It is a plain form: no script runs here.
var addHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "add.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel"><a href="/team/{{.Token}}" class="back">←</a> {{.Team.Name}}</h1>
  <h2>{{T "add.head"}}</h2>
</div>

{{- if .Notice.Text}}
<p class="note{{if .Notice.Bad}} lost{{end}}">{{.Notice.Text}}</p>
{{- end}}

<div class="card">
  <div class="top">
    <div class="who prose">
      <div class="name">{{T "add.card.title"}}</div>
      <div class="meta plain">{{T "add.card.why"}}</div>
    </div>
    <form method="post" action="/team/{{.Token}}/add">
      <button class="btn" type="submit">{{T "add.start"}}</button>
    </form>
  </div>
  <ol class="steps prose">
    <li>{{T "add.step.1"}}</li>
    <li>{{T "add.step.2"}}</li>
    <li>{{T "add.step.3"}}</li>
    <li>{{T "add.step.4"}}</li>
  </ol>
</div>

<p class="note prose">{{T "add.note"}}</p>

</div></body></html>
`

// signInHTML is /team/<token>/signin/<id>: the vendor's link, the field for
// what the browser ended on, and the way out.
var signInHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "signin.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel"><a href="/team/{{.Token}}" class="back">←</a> {{.Team.Name}}</h1>
  <h2>{{T "add.head"}}</h2>
</div>

{{- if .Notice.Text}}
<p class="note{{if .Notice.Bad}} lost{{end}}">{{.Notice.Text}}</p>
{{- end}}

{{- if .SignIn.Done}}
<div class="card prose">
  {{- if .SignIn.Again}}
  <p class="step">{{T "signin.done.again" (args "User" .SignIn.User "Plan" (signInPlan .SignIn))}}</p>
  {{- else}}
  <p class="step">{{if .SignIn.Plan}}{{T "signin.done.plan" (args "User" .SignIn.User "Plan" .SignIn.Plan)}}{{else}}{{T "signin.done" (args "User" .SignIn.User)}}{{end}}</p>
  {{- end}}
  <p class="note">{{T "signin.done.pool"}}</p>
  <p><a class="btn" href="/team/{{.Token}}">{{T "signin.back"}}</a></p>
</div>
{{- else if .SignIn.Waiting}}
<div class="card prose">
  <p class="step">{{T "signin.open"}}</p>
  <p class="link mono sel">{{.SignIn.URL}}</p>

  <p class="step">{{T "signin.paste"}}</p>
  <form method="post" action="/team/{{.Token}}/signin/{{.SignIn.ID}}/callback">
    <input class="field" type="text" name="url" autocomplete="off" autocapitalize="off"
           autocorrect="off" spellcheck="false" placeholder="http://localhost:…/callback?code=…&state=…" required>
    <button class="btn" type="submit">{{T "signin.finish"}}</button>
  </form>
  <p class="note">{{T "signin.until"}}</p>
</div>
<form method="post" action="/team/{{.Token}}/signin/{{.SignIn.ID}}/cancel">
  <button class="btn quiet" type="submit">{{T "signin.cancel"}}</button>
</form>
{{- else}}
<div class="card prose">
  <p class="step">{{T "signin.state" (args "State" (signInWords .SignIn))}}</p>
  {{- if .SignIn.Error}}
  <p class="note lost">{{.SignIn.Error}}</p>
  {{- end}}
  <p><a class="btn" href="/team/{{.Token}}/add">{{T "signin.again"}}</a></p>
</div>
{{- end}}

</div></body></html>
`

// usageHTML is /team/<token>/usage: what the team is spending, per account
// and against the team key's own budget.
//
// Two sections and not one table: an account's allowance is the vendor's
// reading of that subscription, and the key's budget is magpie's count of
// what the team's requests spent. Adding them together would be a number
// that means nothing.
var usageHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "usage.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel"><a href="/team/{{.Token}}" class="back">←</a> {{.Team.Name}}</h1>
  <h2>{{T "usage.head"}}</h2>
</div>
<div class="sum">
  <span>{{T "accounts.group" (args "Name" .Group.Name "ID" .Group.ID)}}</span>
  {{- if .UsageReadAt}}
  <span>{{T "accounts.read" (args "Ago" (ago .Now .UsageReadAt))}}</span>
  {{- else}}
  <span>{{T "usage.no_read"}}</span>
  {{- end}}
  <span>{{T "usage.budget_read" (args "Ago" (ago .Now .KeyReadAt))}}</span>
</div>

<h2 class="sect">{{T "usage.budget.head"}}{{if .Key}} <span class="mono sel">({{.Key.Name}} · {{.Key.ID}})</span>{{end}}</h2>
{{- if .KeyErr}}
<p class="note warn">{{T "usage.budget.err" (args "Why" .KeyErr)}}</p>
{{- else if not .Limits}}
<div class="card prose">
  <div class="name">{{T "usage.nocap.title"}}</div>
  <p class="note">{{T "usage.nocap.why" (args "Name" .Key.Name)}}</p>
</div>
{{- else}}
<div class="cards">
  {{- range .Limits}}
  <section class="card">
    <div class="top">
      <div class="who">
        <div class="name">{{limitWord .}}</div>
        <div class="meta">{{if .Models}}{{Tn "usage.models.only" (len .Models) (args "N" (len .Models))}}{{else}}{{T "usage.models.every"}}{{end}}{{if .CacheReads}} · {{T "usage.cache_counted"}}{{end}}</div>
      </div>
      {{- if .Spent}}<span class="chip lost">{{T "usage.chip.spent"}}</span>
      {{- else}}<span class="chip on">{{T "usage.chip.within"}}</span>{{end}}
    </div>

    {{- if .HasTokens}}
    <div class="win">
      <div class="labels"><span>{{T "usage.tokens"}}</span><span class="n sel">{{tokens .TokensUsed}} / {{tokens .Tokens}}</span></div>
      <div class="track{{if ge .TokenShare 100}} full{{end}}"><i style="width:{{.TokenShare}}%"></i></div>
      <div class="reset"><span>{{T "usage.left" (args "Value" (tokens .TokensLeft))}}</span></div>
    </div>
    {{- end}}
    {{- if .HasCost}}
    <div class="win">
      <div class="labels"><span>{{T "usage.cost"}}</span><span class="n sel">{{cost .CostUsed}} / {{cost .Cost}}</span></div>
      <div class="track{{if ge .CostShare 100}} full{{end}}"><i style="width:{{.CostShare}}%"></i></div>
      <div class="reset"><span>{{T "usage.left" (args "Value" (cost .CostLeft))}}</span></div>
    </div>
    {{- end}}

    <div class="reset"><span>{{T "usage.starts" (args "At" (resets . $.Now))}}</span></div>
    <p class="note">{{Tn "usage.calls" .Calls (args "N" .Calls)}}{{if .Unpriced}} · {{T "usage.unpriced" (args "N" .Unpriced)}}{{end}}</p>
  </section>
  {{- end}}
</div>
{{- end}}

<h2 class="sect">{{T "usage.accounts.head"}}</h2>
{{- if .AccountsErr}}
<p class="note warn">{{.AccountsErr}}.</p>
{{- end}}
{{- if not .Accounts}}
<div class="empty">
  <p class="big">{{T "accounts.empty"}}</p>
  <p>{{T "accounts.empty.why" (args "Name" .Group.Name)}}</p>
</div>
{{- else}}
<div class="cards">
  {{- range .Accounts}}
  <section class="card{{if not .On}} off{{end}}">
    <div class="top">
      <div class="who">
        <div class="name sel">{{if .Name}}{{.Name}}{{else}}{{.Entry}}{{end}}</div>
        {{- if .Name}}
        <div class="meta">{{if .Plan}}<span class="plan">{{.Plan}}</span> · {{end}}<span class="mono sel">{{.Entry}}</span></div>
        {{- end}}
      </div>
      {{- if .Lost}}<span class="chip lost">{{T "usage.chip.lost"}}</span>
      {{- else if .Key}}<span class="chip key">{{T "usage.chip.key"}}</span>
      {{- else if not .Reading}}<span class="chip off">{{T "usage.chip.unread"}}</span>
      {{- else if not .On}}<span class="chip off">{{T "usage.chip.idle"}}</span>
      {{- else if .Spent}}<span class="chip paused">{{T "usage.chip.usedup"}}</span>
      {{- else}}<span class="chip on">{{T "usage.chip.share" (args "N" .ShareInt)}}</span>{{end}}
    </div>

    {{- if .Reading}}
      {{- $r := .Reading}}
      {{- $w := windows $r $.Now}}
      {{- if $w}}
      <div class="wins{{if eq (len $w) 1}} one{{end}}">
        {{- range $w}}
        <div class="win">
          <div class="labels">
            <span>{{.Name}}</span>
            {{- if .Text}}<span class="n sel">{{if .Display}}{{.Display}}{{else}}—{{end}}</span>
            {{- else}}<span class="n sel">{{.ShareInt}}%</span>{{end}}
          </div>
          {{- if not .Text}}
          <div class="track{{if .Empty}} full{{end}}"><i style="width:{{.ShareInt}}%"></i></div>
          {{- end}}
          {{- if .Resets}}
          <div class="reset"><span>{{T "accounts.resets" (args "At" .Resets)}}{{if .ResetsIn}} · {{.ResetsIn}}{{end}}</span></div>
          {{- end}}
        </div>
        {{- end}}
      </div>
      {{- end}}
      {{- if $r.ReadAt}}
      <div class="read">{{T "usage.read" (args "Ago" (ago $.Now $r.ReadAt))}}</div>
      {{- end}}
    {{- end}}

    {{- if .Note}}
    <p class="note">{{.Note}}</p>
    {{- end}}
  </section>
  {{- end}}
</div>
{{- end}}

<h2 class="sect">{{T "usage.member.head"}}</h2>
{{- if .MemberErr}}
<p class="note warn">{{.MemberErr}}.</p>
{{- end}}
{{- if .Members}}
<div class="card">
  <table class="log">
    <thead><tr><th>{{T "usage.member.member"}}</th>{{range .MemberPeriods}}<th>{{T .Key}}</th>{{end}}</tr></thead>
    <tbody>
    {{- range .Members}}
    <tr{{if .Revoked}} class="muted"{{end}}>
      <td data-label="{{T "usage.member.member"}}"><span class="sel">{{.Name}}</span>{{if .Revoked}} <span class="chip off">{{T "usage.member.revoked"}}</span>{{end}}</td>
      <td data-label="{{T "usage.member.today"}}">{{memberCell .Today}}</td>
      <td data-label="{{T "usage.member.week"}}">{{memberCell .Week}}</td>
      <td data-label="{{T "usage.member.month"}}">{{memberCell .Month}}</td>
    </tr>
    {{- end}}
    </tbody>
    <tfoot>
    <tr>
      <td data-label="{{T "usage.member.member"}}"><span class="sel">{{T "usage.member.total"}}</span></td>
      <td data-label="{{T "usage.member.today"}}">{{memberCell .MemberTotals.Today}}</td>
      <td data-label="{{T "usage.member.week"}}">{{memberCell .MemberTotals.Week}}</td>
      <td data-label="{{T "usage.member.month"}}">{{memberCell .MemberTotals.Month}}</td>
    </tr>
    </tfoot>
  </table>
</div>
{{- else if not .MemberErr}}
<div class="empty">
  <p class="big">{{T "usage.member.none"}}</p>
  <p>{{T "usage.member.none.why" (args "Members" (membersURL .Token))}}</p>
</div>
{{- end}}

</div></body></html>
`

// memberCell is one member key's cell: what it used over the period, or a
// dash when it used nothing there. A period whose read failed is a dash too:
// a zero would be a figure nobody read.
func memberCell(s SpendTotals) string {
	if !s.Read || !s.Used() {
		return "—"
	}
	return itoa(s.Calls) + " · " + fmtTokens(int64(s.Tokens())) + " tokens · " + fmtCost(s.Cost)
}

// connectHTML is /team/<token>/connect: what a member needs to reach the
// team's pool, and how to tell it worked.
var connectHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "connect.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel"><a href="/team/{{.Token}}" class="back">←</a> {{.Team.Name}}</h1>
  <h2>{{T "connect.head"}}</h2>
</div>
<div class="sum">
  <span>{{T "connect.sum.send"}}</span>
  {{- if .Gateway}}
  <span>{{T "connect.sum.pool" (args "URL" .Gateway)}}</span>
  {{- end}}
</div>

{{- if .GatewayErr}}
<p class="note warn">{{.GatewayErr}}</p>
{{- else}}

<h2 class="sect">{{T "connect.s1"}}</h2>
<div class="card prose">
  <p class="step">{{T "connect.s1.step"}}</p>
  <div class="code"><pre class="sel">{{env .}}</pre></div>
  <p class="note">{{if .HasKey}}{{T "connect.s1.key"}}{{else}}{{T "connect.s1.placeholder"}}{{end}}</p>
</div>

<h2 class="sect">{{T "connect.s2"}}</h2>
<div class="card prose">
  <p class="step">{{T "connect.s2.step"}}</p>
  <div class="code"><pre class="sel">{{json .}}</pre></div>
</div>

<h2 class="sect">{{T "connect.s3"}}</h2>
<div class="card prose">
  <p class="step">{{T "connect.s3.step"}}</p>
  <div class="code"><pre class="sel">{{check .}}</pre></div>
  <p class="step">{{T "connect.s3.when"}}</p>
  <div class="code"><pre class="sel">{{expected .}}</pre></div>
  <p class="note">{{T "connect.s3.then" (args "Model" (model .))}}</p>
</div>

<h2 class="sect">{{T "connect.s4"}}</h2>
<div class="card">
  <div class="prose">
    <dl class="fails">
      <dt><span class="mono">401</span> · {{T "connect.fail.401"}}</dt>
      <dd>{{T "connect.fail.401.why"}}</dd>
      <dt><span class="mono">403</span> · {{T "connect.fail.403"}}</dt>
      <dd>{{T "connect.fail.403.why"}}</dd>
      <dt><span class="mono">429</span> · {{T "connect.fail.429"}}</dt>
      <dd>{{T "connect.fail.429.why"}}</dd>
      <dt>{{T "connect.fail.hang"}}</dt>
      <dd>{{T "connect.fail.hang.why"}}</dd>
      <dt>{{T "connect.fail.other"}}</dt>
      <dd>{{T "connect.fail.other.why"}}</dd>
    </dl>
  </div>
</div>

{{- end}}

{{- if .KeyNote $.Trans}}
<p class="note warn">{{.KeyNote $.Trans}}</p>
{{- end}}

<h2 class="sect">{{if .Models}}{{T "connect.models.head.key"}}{{else}}{{T "connect.models.head"}}{{end}}</h2>
{{- if .ModelsErr}}
<p class="note warn">{{.ModelsErr}}.</p>
{{- else if not .Models}}
<p class="note">{{T "connect.models.none"}}</p>
{{- else}}
<div class="card prose">
  <p class="step">{{Tn "connect.models.step" (len .Models) (args "N" (len .Models) "More" (modelsMore .Trans .ModelsPartial))}}</p>
  <div class="chips">
    {{- range .Models}}<span class="chip mono sel">{{.}}</span>
    {{- end}}
  </div>
</div>
{{- end}}

</div></body></html>
`

// auditHTML is /team/<token>/audit: what this team's lead did through the
// portal, and what the portal turned away.
//
// A refused line is drawn as loudly as allowed one: the page exists so a
// lead can see that something with their link asked for an account that
// isn't theirs.
var auditHTML = `<!doctype html>
<html lang="{{.Trans.HTML}}"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{T "audit.title" (args "Team" .Team.Name)}}</title>
<style>` + pageStyle + `</style>
</head><body><div class="wrap">

<div class="head">
  <h1 class="sel"><a href="/team/{{.Token}}" class="back">←</a> {{.Team.Name}}</h1>
  <h2>{{T "audit.head"}}</h2>
</div>
<div class="sum">
  <span>{{Tn "audit.changes" .Total (args "N" .Total)}}</span>
  {{- if gt .Total (len .Rows)}}
  <span>{{T "audit.shown" (args "N" (len .Rows))}}</span>
  {{- end}}
</div>

{{- if not .Rows}}
<div class="empty">
  <p class="big">{{T "audit.empty"}}</p>
  <p>{{T "audit.empty.why"}}</p>
</div>
{{- else}}
<div class="card">
  <table class="log">
    <thead><tr><th>{{T "audit.th.when"}}</th><th>{{T "audit.th.what"}}</th><th>{{T "audit.th.account"}}</th><th>{{T "audit.th.by"}}</th><th></th></tr></thead>
    <tbody>
    {{- range .Rows}}
    <tr{{if .Refused}} class="refused"{{end}}>
      <td class="when" data-label="{{T "audit.th.when"}}"><span class="sel">{{.At.Local.Format "01-02 15:04"}}</span> <span class="muted">{{ago $.Now .At}}</span></td>
      <td data-label="{{T "audit.th.what"}}">{{.Verb}}</td>
      <td data-label="{{T "audit.th.account"}}">{{if .Name}}<span class="sel">{{.Name}}</span>{{if .Account}} <span class="mono muted">{{.Account}}</span>{{end}}{{else if .Account}}<span class="mono sel">{{.Account}}</span>{{else}}<span class="muted">—</span>{{end}}</td>
      <td class="muted" data-label="{{T "audit.th.by"}}">{{.Who}}</td>
      <td class="mark">{{if .Refused}}<span class="chip lost">{{T "audit.refused"}}</span>{{end}}</td>
    </tr>
    {{- end}}
    </tbody>
  </table>
</div>
{{- end}}

<p class="note prose">{{T "audit.note"}}</p>

</div></body></html>
`
