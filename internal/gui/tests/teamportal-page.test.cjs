// Models › Team portal: the teams this magpie serves, made from magpie's
// own account groups and gateway keys (plugins/teamportal, internal/gui
// ops.go). A team whose key reaches every account says so, and is held to
// its group with one click; a team is made by picking a group, a key and
// how long its link lasts, and its link is shown once with its end; New
// link asks how long the new one lasts, starting at the old one's, and
// says the old one stops; a link with an end says when, and one past it
// says so; Remove asks first.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const words = {
  en: { nameL: "Name", groupL: "Account group", keyL: "Gateway key", make: "Make a team", makeThe: "Make the team", warn: "Reaches every account", noLink: "No link yet", newLink: "New link", group: "Pick an account group", key: "Pick a gateway key", held: "old's key now reaches its group's accounts only", lifeL: "Link lifetime", never: "Never expires", d30: "30 days", d90: "90 days", expired: "Link expired", ends: "Link expires", stops: "This link stops working on", stopsNow: "stops working at once" },
  zh: { nameL: "名称", groupL: "账号组", keyL: "网关密钥", make: "新建团队", makeThe: "新建", warn: "能用所有账号", noLink: "还没有链接", newLink: "新链接", group: "选择账号组", key: "选择网关密钥", held: "old 的密钥现在只能用它所在组的账号", lifeL: "链接有效期", never: "永久有效", d30: "30 天", d90: "90 天", expired: "链接已过期", ends: "失效", stops: "之后失效", stopsNow: "立即失效" },
  "zh-TW": { nameL: "名稱", groupL: "帳號組", keyL: "閘道金鑰", make: "新增團隊", makeThe: "新增", warn: "能用所有帳號", noLink: "還沒有連結", newLink: "新連結", group: "選擇帳號群組", key: "選擇閘道金鑰", held: "old 的金鑰現在只能用它所在群組的帳號", lifeL: "連結有效期", never: "永久有效", d30: "30 天", d90: "90 天", expired: "連結已過期", ends: "失效", stops: "之後失效", stopsNow: "立即失效" },
  ja: { nameL: "名前", groupL: "アカウントグループ", keyL: "ゲートウェイキー", make: "チームを作成", makeThe: "作成", warn: "すべてのアカウントを使える", noLink: "リンクはまだありません", newLink: "新しいリンク", group: "アカウントグループを選択", key: "ゲートウェイキーを選択", held: "old のキーはグループのアカウントだけを使うようになりました", lifeL: "リンクの有効期間", never: "無期限", d30: "30 日", d90: "90 日", expired: "リンクの期限切れ", ends: "に失効", stops: "以降使えなくなります", stopsNow: "すぐに使えなくなります" },
  de: { nameL: "Name", groupL: "Kontogruppe", keyL: "Zugangsschlüssel", make: "Team anlegen", makeThe: "Anlegen", warn: "Erreicht jedes Konto", noLink: "Noch kein Link", newLink: "Neuer Link", group: "Kontogruppe wählen", key: "Gateway-Schlüssel wählen", held: "Der Schlüssel von old erreicht jetzt nur noch die Konten seiner Gruppe", lifeL: "Gültigkeit des Links", never: "Läuft nie ab", d30: "30 Tage", d90: "90 Tage", expired: "Link abgelaufen", ends: "läuft am", stops: "funktioniert ab", stopsNow: "funktioniert sofort nicht mehr" },
};

// ops is magpie's side of the page: /ops/teams/*, as internal/gui/ops.go
// answers it, over a list the test can read back
function ops(writes) {
  const list = {
    teams: [
      { id: "ec-team", name: "ec-team", groupId: "g1", groupName: "ec", keyId: "k1", keyName: "ec", linked: true, accounts: 2, held: true, expiresAt: "2027-01-15T03:00:00Z", lifetime: "30d" },
      { id: "late", name: "late", groupId: "g4", groupName: "late", keyId: "k4", keyName: "late", linked: true, accounts: 1, held: true, expiresAt: "2026-11-01T03:00:00Z", expired: true, lifetime: "7d" },
      { id: "old", name: "old", groupId: "g2", groupName: "legacy", keyId: "k2", keyName: "legacy", linked: false, accounts: 0, held: false },
    ],
    groups: [{ id: "g1", name: "ec", accounts: 2 }, { id: "g2", name: "legacy", accounts: 0 }, { id: "g3", name: "payments", accounts: 3 }],
    keys: [{ id: "k1", name: "ec", issues: true }, { id: "k2", name: "legacy", issues: false }, { id: "k3", name: "payments-key", issues: true }],
  };
  // when a link made now for ttl ends, as ops.go answers it: none for forever
  const ends = (ttl) => ({ "7d": "2026-11-30T03:00:00Z", "30d": "2026-12-23T03:00:00Z", "90d": "2027-02-21T03:00:00Z" })[ttl];
  return async (route) => {
    const req = route.request(), what = new URL(req.url()).pathname.replace("/ops/teams/", "");
    const json = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (req.method() === "GET" && what === "list") return json(list);
    const body = JSON.parse(req.postData() || "{}");
    writes.push({ what, body, origin: new URL(req.url()).origin });
    const team = list.teams.find((x) => x.id === body.id);
    switch (what) {
      case "add":
        list.teams.push({ id: body.name, name: body.name, groupId: body.group, groupName: "payments", keyId: body.key, keyName: "payments-key", linked: true, accounts: 3, held: true, expiresAt: ends(body.ttl), lifetime: body.ttl });
        return json({ id: body.name, name: body.name, link: "/team/tok-made", expiresAt: ends(body.ttl) });
      case "token":
        Object.assign(team, { linked: true, expired: false, expiresAt: ends(body.ttl), lifetime: body.ttl });
        return json({ id: team.id, name: team.name, link: "/team/tok-new", expiresAt: ends(body.ttl) });
      case "hold":
        team.held = true;
        return json({ id: team.id, name: team.name });
      case "remove":
        list.teams = list.teams.filter((x) => x !== team);
        return json({ id: team.id, left: team.accounts });
    }
    return json({ error: "no such action" }, 404);
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1100, 420]) {
    for (const [lang, w] of Object.entries(words)) {
      test(`${engine} ${width} ${lang}: teams are made, held, given a new link and removed from the page`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const events = [], errors = [], writes = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, "light", events, { lan: true }));
        // the page's own routes, over the fixture's, and a boot that says a
        // portal runs beside this magpie, as MAGPIE_TEAMD_DIR does
        await page.route("**/ops/teams/**", ops(writes));
        await page.route("**/boot.js", (route) => route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: false, teamPortal: true })};` }));
        await page.goto("http://magpie.test/?view=teamportal");
        const row = (id) => page.locator(`#tpTeams .acc[data-team="${id}"]`);
        await row("old").waitFor();
        const editor = page.locator("#modal .editor");
        const ok = () => page.locator("#modal .editor .bar .primary");
        const accept = () => page.locator("dialog.action-confirm .primary");

        // the sidebar has the page, beside Gateway, and it opens from there
        const nav = page.locator('#nav button[data-view="teamportal"]');
        assert.ok(await nav.isVisible(), "no Team portal in the sidebar");
        assert.equal(await page.evaluate(() => document.querySelector('#nav button[data-view="gateway"]').nextElementSibling?.dataset.view), "teamportal");
        if (width >= 1100) {
          await page.locator('#nav button[data-view="gateway"]').click();
          await page.locator("#view-teamportal").waitFor({ state: "hidden" });
          await nav.click();
          await page.locator("#view-teamportal").waitFor();
          await row("old").waitFor();
        }

        // a key that reaches every account says so; a held one doesn't
        assert.equal(await row("ec-team").getByText(w.warn, { exact: true }).count(), 0);
        assert.equal((await row("old").getByRole("button", { name: w.warn, exact: true }).textContent()).trim(), w.warn);
        assert.ok((await row("old").getByRole("button", { name: w.warn }).getAttribute("title")).length > 20, "the warning doesn't say what it means");
        assert.equal(await row("old").getByText(w.noLink, { exact: true }).count(), 1);
        assert.equal(await row("ec-team").getByText(w.noLink, { exact: true }).count(), 0);
        // a link with an end says when; one past it says so, as a warning;
        // one with no link says neither
        const said = (id) => row(id).evaluate((r) => r.textContent);
        assert.ok((await said("ec-team")).includes(w.ends), `ec-team's row doesn't say when its link ends: ${await said("ec-team")}`);
        assert.ok(!(await said("ec-team")).includes(w.expired), "a working link is said to be expired");
        assert.equal((await row("late").locator(".amodels.tp-wide").textContent()).trim(), w.expired);
        assert.ok(!(await said("old")).includes(w.expired) && !(await said("old")).includes(w.ends), "a team with no link says its link ends");

        // held with one click
        await row("old").getByRole("button", { name: w.warn }).click();
        await page.waitForFunction(() => !document.querySelector('#tpTeams .acc[data-team="old"] .amodels.set'));
        assert.deepEqual(writes.at(-1), { what: "hold", body: { id: "old" }, origin: "http://magpie.test" });
        assert.equal((await page.locator("#status").textContent()).trim(), w.held);

        // a team made: a name, a group, a key and how long its link lasts,
        // then its link once
        await page.getByRole("button", { name: w.make, exact: true }).click();
        await editor.waitFor();
        // each field's name is one cell and its control the next, level with
        // it, and no name is cut off (the editor is a two-column grid:
        // openModal puts its fields in .ebody, which is that grid)
        const formRows = () => editor.evaluate((ed) => {
          const box = ed.querySelector(":scope > .ebody") || ed;
          const mid = (r) => r.top + r.height / 2;
          const out = [];
          for (const l of box.querySelectorAll(":scope > label")) {
            const c = l.nextElementSibling;
            const lr = l.getBoundingClientRect(), cr = c.getBoundingClientRect();
            out.push({ label: l.textContent.trim(), off: Math.round((mid(lr) - mid(cr)) * 10) / 10,
              over: l.scrollWidth - l.clientWidth, side: lr.right <= cr.left + 1 });
          }
          return out;
        });
        const levelRows = (rows, want) => {
          assert.deepEqual(rows.map((r) => r.label), want, `the form's rows: ${JSON.stringify(rows)}`);
          for (const r of rows) {
            assert.ok(r.side, `${r.label} isn't beside its control`);
            assert.ok(Math.abs(r.off) <= 2, `${r.label} sits ${r.off}px off its control`);
            assert.ok(r.over <= 0, `the name "${r.label}" is cut off by ${r.over}px`);
          }
        };
        levelRows(await formRows(), [w.nameL, w.groupL, w.keyL, w.lifeL]);
        // the four controls are one form's fields: the same height, and a
        // box drawn round each, the pickers as much as the text field
        const boxes = await editor.evaluate((ed) => {
          const box = ed.querySelector(":scope > .ebody") || ed;
          return [...box.querySelectorAll(":scope > label + *")].map((c) => {
            const r = c.getBoundingClientRect(), s = getComputedStyle(c);
            return { h: Math.round(r.height), border: s.borderTopWidth, opacity: s.opacity };
          });
        });
        assert.equal(boxes.length, 4, JSON.stringify(boxes));
        assert.equal(new Set(boxes.map((b) => b.h)).size, 1, `the fields are different heights: ${JSON.stringify(boxes)}`);
        for (const b of boxes) {
          assert.ok(parseFloat(b.border) >= 1, `a field has no border: ${JSON.stringify(b)}`);
          assert.equal(b.opacity, "1", `a field is faded: ${JSON.stringify(b)}`);
        }
        // nothing is sent until a group and a key are picked
        await editor.locator("input").fill("payments");
        await ok().click();
        await page.locator(".proto-menu").waitFor();
        await page.keyboard.press("Escape");
        assert.equal(writes.filter((x) => x.what === "add").length, 0, "an add was sent with no group");
        await editor.locator("button.amodels", { hasText: w.group }).click();
        await page.locator(".proto-menu .pm-item", { hasText: "payments" }).first().click();
        await editor.locator("button.amodels", { hasText: w.key }).click();
        await page.locator(".proto-menu .pm-item", { hasText: "payments-key" }).click();
        // the link's lifetime starts at none, and 30 days is picked
        const life = editor.locator("button.amodels", { hasText: w.never });
        assert.equal(await life.count(), 1, "the lifetime doesn't start at no end");
        await life.click();
        await page.locator(".proto-menu .pm-item", { hasText: w.d30 }).click();
        assert.equal(await editor.locator("button.amodels", { hasText: w.d30 }).count(), 1, "the picked lifetime isn't shown");
        assert.equal((await ok().textContent()).trim(), w.makeThe);
        await ok().click();
        await page.waitForFunction(() => document.querySelector("#modal .editor .mono")?.textContent.includes("/team/tok-made"));
        assert.deepEqual(writes.at(-1).body, { name: "payments", group: "g3", key: "k3", ttl: "30d" });
        assert.equal((await editor.locator(".mono").textContent()).trim(), "http://magpie.test/team/tok-made");
        assert.ok((await editor.textContent()).includes(w.stops), "the made link doesn't say when it ends");
        await ok().click();
        await row("payments").waitFor();

        // New link asks how long, starting at the link's own lifetime, and
        // says the link that works now stops
        await row("ec-team").getByRole("button", { name: w.newLink, exact: true }).click();
        await editor.waitFor();
        assert.equal(await editor.locator("button.amodels", { hasText: w.d30 }).count(), 1, "New link doesn't start at the link's own lifetime");
        // its one field is an editor's row too, its name level with the picker
        levelRows(await formRows(), [w.lifeL]);
        assert.ok((await editor.textContent()).includes(w.stopsNow), "New link doesn't say the old link stops");
        assert.equal(writes.filter((x) => x.what === "token").length, 0, "a new link was made before it was asked");
        await editor.locator("button.amodels", { hasText: w.d30 }).click();
        await page.locator(".proto-menu .pm-item", { hasText: w.d90 }).click();
        await ok().click();
        await page.waitForFunction(() => document.querySelector("#modal .editor .mono")?.textContent.endsWith("/team/tok-new"));
        assert.deepEqual(writes.at(-1).body, { id: "ec-team", ttl: "90d" });
        assert.ok((await editor.textContent()).includes(w.stops), "the new link doesn't say when it ends");
        await ok().click();
        await editor.waitFor({ state: "detached" });

        // a link past its end has nothing left to stop, and Cancel sends nothing
        await row("late").getByRole("button", { name: w.newLink, exact: true }).click();
        await editor.waitFor();
        assert.ok(!(await editor.textContent()).includes(w.stopsNow), "New link on an expired link says a working link stops");
        await editor.locator(".bar button:not(.primary)").click();
        await editor.waitFor({ state: "detached" });

        // Remove asks first too
        await row("old").locator("button.copy").click();
        await accept().click();
        await row("old").waitFor({ state: "detached" });
        assert.deepEqual(writes.map((x) => x.what), ["hold", "add", "token", "remove"]);
        // the page's own origin: Sec-Fetch-Site, which ops.go checks, is the
        // network's to add and never reaches route(); TestOpsPageNeedsThePageItself
        // holds the check itself
        assert.ok(writes.every((x) => x.origin === "http://magpie.test"), "a write left the page's own origin");

        const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert.ok(over <= 0, `the page scrolls sideways by ${over}px`);
        assert.equal(await page.locator("select").count(), 0, "no native select");
        assert.deepEqual(errors, []);
      });
    }
  }
}

// A magpie of one person's runs no portal beside it: no entry in the
// sidebar, as its boot.js says nothing of a portal.
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: no Team portal where no portal runs`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 1100, height: 700 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [], asked = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("request", (r) => { if (new URL(r.url()).pathname.startsWith("/ops/")) asked.push(r.url()); });
    await page.route("**/*", fixture("en", "light", [], { lan: true }));
    await page.goto("http://magpie.test/?view=gateway");
    await page.locator("#gatewayKeys .acc[data-key]").first().waitFor();
    assert.equal(await page.locator('#nav button[data-view="teamportal"]').count(), 0);
    assert.deepEqual(asked, [], "the page asked for teams it has no portal for");
    assert.deepEqual(errors, []);
  });
}

// On a large screen the page keeps the width every page keeps, centred
// where Gateway is (page-width.test.cjs holds the sidebar's own pages to
// it; this one is the sidebar's only where a portal runs, so it is held
// here).
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the page keeps one width at 1920`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 1920, height: 900 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", fixture("en", "light", [], { lan: true }));
    await page.route("**/ops/teams/**", ops([]));
    await page.route("**/boot.js", (route) => route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang: "en", theme: "light", web: false, teamPortal: true })};` }));
    const box = (v) => page.evaluate((v) => {
      const e = document.querySelector(`#view-${v}`), cs = getComputedStyle(e), r = e.getBoundingClientRect();
      const pl = parseFloat(cs.paddingLeft), pr = parseFloat(cs.paddingRight);
      return { left: Math.round(r.left + pl), width: Math.round(e.clientWidth - pl - pr) };
    }, v);
    await page.goto("http://magpie.test/?view=gateway");
    await page.locator("#gatewayKeys .acc[data-key]").first().waitFor();
    const gw = await box("gateway");
    await page.locator('#nav button[data-view="teamportal"]').click();
    await page.locator('#tpTeams .acc[data-team="old"]').waitFor();
    const tp = await box("teamportal");
    assert.ok(tp.width <= 1200 && tp.width >= 1150, `the page is ${tp.width} wide at 1920`);
    assert.ok(Math.abs(tp.left - gw.left) <= 1, `the page starts at ${tp.left}, Gateway at ${gw.left}`);
  });
}
