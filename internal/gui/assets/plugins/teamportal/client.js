// Models › Team portal: the teams this magpie serves beside its owner's
// own, each an account group and a gateway key held to it, and the link a
// team's lead opens the portal with.
//
// A page the teamportal plugin adds to the window (kernel.js), its words in
// i18n.js beside this file. Its routes are magpie's own, under /ops/ and
// behind the window's sign-in (internal/gui/ops.go): not under /api, so the
// management API never reaches them. Making a team holds its key to its
// group there (teamcore's Ops.Add), so the accounts a lead sees and the
// accounts the key can spend are one list; a team made before that, whose
// key still reaches every account, says so and is held with one click.

// tpAsk is one of the page's routes: a read, or a write of body as JSON.
async function tpAsk(path, body) {
  const res = await fetch("/ops/teams/" + path, body === undefined ? {} : {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { throw new Error(text || `${res.status} ${res.statusText}`); }
  if (!res.ok) throw new Error(data?.error || `${res.status} ${res.statusText}`);
  return data;
}

// the page's last reading, and whether a write is on its way
let tpList = null, tpBusy = false;

const TP_REMOVE = "M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5M7 7v4M9 7v4";

async function tpLoad() {
  try { tpList = await tpAsk("list"); } catch (e) { tpList = { teams: [], groups: [], keys: [], error: e.message }; }
  tpDraw();
}

// tpWrite sends one write at a time, says how it went, and reads the
// list again; its answer, or null when it failed
async function tpWrite(path, body) {
  if (tpBusy) return null;
  tpBusy = true;
  try {
    return await tpAsk(path, body);
  } catch (e) {
    status(t(e.message), "err");
    return null;
  } finally {
    tpBusy = false;
    tpLoad();
  }
}

const tpAccounts = (n) => n === 1 ? t("1 account") : n ? t("{n} accounts", { n }) : t("No accounts");

function tpDraw() {
  const box = $("#tpTeams");
  if (!box || !tpList) return;
  $("#tpAdd").onclick = () => tpMake();
  const list = el("div", "accts");
  for (const tm of tpList.teams) {
    const row = el("div", "acc in-use");
    row.dataset.team = tm.id;
    const where = [tm.groupName || tm.groupId, tm.keyName || tm.keyId];
    if (tm.accounts != null) where.push(tpAccounts(tm.accounts));
    row.append(el("span", "n", tm.name), el("span", "plan", where.join(" · ")), el("span", "grow"));
    // a key that names no account group spends every account magpie has
    if (tm.held === false) {
      const warn = el("button", "amodels set tp-wide", t("Reaches every account"));
      warn.type = "button";
      warn.title = t("This team's key names no account group, so it reaches every account in magpie, not only this team's. Click to hold it to the team's group.");
      warn.onclick = async () => {
        if (await tpWrite("hold", { id: tm.id })) status(t("{name}'s key now reaches its group's accounts only", { name: tm.name }), "ok");
      };
      row.append(warn);
    }
    if (!tm.linked) row.append(el("span", "amodels", t("No link yet")));
    const link = el("button", "text", t("New link"));
    link.type = "button";
    link.onclick = async () => {
      if (tm.linked && !(await confirmAction(t("Make a new link for {name}?", { name: tm.name }), t("The link the team has now stops working at once."), t("Make a new link")))) return;
      const out = await tpWrite("token", { id: tm.id });
      if (out) tpShowLink(out);
    };
    const rm = el("button", "copy");
    rm.type = "button";
    rm.title = t("Remove");
    rm.setAttribute("aria-label", t("Remove"));
    rm.append(svg(TP_REMOVE, 14, 1.6));
    rm.onclick = async () => {
      if (!(await confirmAction(t("Remove {name}?", { name: tm.name }), t("The team and its link go. Its account group and gateway key stay in magpie as they are."), t("Remove")))) return;
      const out = await tpWrite("remove", { id: tm.id });
      if (out) status(out.left ? t("{name} removed: its group still holds {n} accounts", { name: tm.name, n: out.left }) : t("{name} removed", { name: tm.name }), "ok");
    };
    row.append(link, rm);
    list.append(row);
  }
  if (!tpList.teams.length) list.append(el("div", "none", t("No teams yet")));
  const kids = [list];
  if (tpList.error) kids.unshift(el("p", "usage-note", t(tpList.error)));
  box.replaceChildren(...kids);
}

// tpPick is a button that picks one of opts, as the Gateway page's menus do
function tpPick(label, empty, opts, onPick) {
  const b = el("button", "amodels tp-pick", t(empty));
  b.type = "button";
  b.setAttribute("aria-label", t(label));
  b.setAttribute("aria-haspopup", "menu");
  let value = "";
  b.onclick = (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (b.classList.contains("open")) return closeProtoMenu();
    openProtoMenu(b, opts, value, (v) => {
      value = v;
      const o = opts.find((x) => x.v === v);
      b.textContent = o ? o.name : t(empty);
      b.classList.toggle("set", !!o);
      onPick(v);
    }, label, "sess-menu");
  };
  return b;
}

// tpMake asks for a team's name, group and key, and makes it. The dialog
// is an editor: a two-column grid, the field's name in the first cell and
// its control in the second, so form-align.test.cjs finds every name level
// with its control.
function tpMake() {
  const draft = { name: "", group: "", key: "" };
  const ed = el("div", "editor");
  const head = el("div", "ehead");
  head.append(el("b", "", t("Make a team")));
  const name = input("", t("Name"));
  name.setAttribute("aria-label", t("Name"));
  name.oninput = () => { draft.name = name.value; };
  const groups = tpList.groups.map((g) => ({ v: g.id, name: g.name, note: tpAccounts(g.accounts), literalName: true }));
  const keys = tpList.keys.map((k) => ({ v: k.id, name: k.name, note: k.issues ? "" : t("Can't issue member keys yet"), literalName: true }));
  const group = tpPick("Account group", "Pick an account group", groups, (v) => { draft.group = v; });
  const key = tpPick("Gateway key", "Pick a gateway key", keys, (v) => { draft.key = v; });
  // a name in the first cell, its control in the second
  const field = (label, control) => [el("label", "", t(label)), control];
  const note = el("p", "usage-note tp-note", t("Making the team holds the key to the group: the key then uses that group's accounts and no others."));
  const bar = el("div", "bar");
  const cancel = el("button", "text", t("Cancel"));
  const make = el("button", "text primary", t("Make the team"));
  cancel.type = make.type = "button";
  cancel.onclick = () => closeConfirmAsk();
  make.onclick = async () => {
    if (!draft.name.trim()) return name.focus({ preventScroll: true });
    if (!draft.group) return group.click();
    if (!draft.key) return key.click();
    const out = await tpWrite("add", draft);
    if (!out) return;
    closeConfirmAsk();
    tpShowLink(out);
  };
  name.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") make.click(); else if (e.key === "Escape") closeConfirmAsk(); };
  bar.append(el("span", "grow"), cancel, make);
  ed.append(head, ...field("Name", name), ...field("Account group", group), ...field("Gateway key", key), note, bar);
  confirmAsk = ed;
  openModal(ed);
  name.focus({ preventScroll: true });
}

// tpShowLink shows a team's link the one time it exists: teamd keeps its
// hash only, so a link lost is replaced by New link, never shown again
function tpShowLink(out) {
  const url = (/^https?:$/.test(location.protocol) ? location.origin : "") + out.link;
  const ed = el("div", "editor");
  const head = el("div", "ehead");
  head.append(el("b", "", t("{name}'s link", { name: out.name })));
  const line = el("div", "acc");
  line.append(el("span", "plan mono", url), el("span", "grow"), copyBtn(url, t("Team link")));
  const note = el("p", "usage-note", t("This is the only time the link is shown. Give it to the team's lead: it is what they sign in with."));
  const bar = el("div", "bar");
  const done = el("button", "text primary", t("Done"));
  done.type = "button";
  done.onclick = () => closeConfirmAsk();
  bar.append(el("span", "grow"), done);
  ed.append(head, line, note, bar);
  confirmAsk = ed;
  openModal(ed);
  done.focus({ preventScroll: true });
}

// The sidebar's entry, beside Gateway, only where a portal runs beside
// this magpie (boot.js's teamPortal, from MAGPIE_TEAMD_DIR): a magpie of
// one person's has no teams to make. It is put in before app.js runs, so
// app.js takes it as one of the sidebar's own (its click, its place in the
// reader's order, its tab on a phone).
if (window.bootPrefs?.teamPortal) {
  const gw = document.querySelector('#nav button[data-view="gateway"]');
  if (gw) {
    const b = document.createElement("button");
    b.dataset.view = "teamportal";
    b.dataset.feature = "teamportal";
    b.innerHTML = '<svg class="nav-ic" viewBox="0 0 20 20" width="16" height="16" aria-hidden="true"><g fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><circle cx="7.5" cy="7" r="2.5"/><circle cx="13.5" cy="8" r="2"/><path d="M3 16c.5-2.6 2.3-4 4.5-4s4 1.4 4.5 4M12.5 12.2c2-.4 3.9.8 4.5 3.3"/></g></svg><span data-t>Team portal</span>';
    gw.after(b);
  }
}

magpie.plugin("teamportal", (ctx) => {
  ctx.slots.add("page", {
    key: "teamportal",
    title: "Team portal",
    html: `
<div class="row-head"><span class="label" data-t>Teams on this magpie</span><span class="grow"></span><button class="text" id="tpAdd" data-t>Make a team</button></div>
<p class="usage-note" data-t>Each team is an account group and a gateway key held to it. Its lead opens the team's link and manages that group's accounts only; this page is yours alone.</p>
<section id="tpTeams"></section>
`,
    open: () => tpLoad(),
    refresh: () => tpLoad(),
    reload: () => tpLoad(),
  });
});
