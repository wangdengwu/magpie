// Models › Team portal: the teams this magpie serves, made from magpie's
// own account groups and gateway keys (plugins/teamportal, internal/gui
// ops.go). A team whose key reaches every account says so, and is held to
// its group with one click; a team is made by picking a group and a key,
// and its link is shown once; New link asks first, as the old one stops;
// Remove asks first too.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const words = {
  en: { make: "Make a team", makeThe: "Make the team", warn: "Reaches every account", noLink: "No link yet", newLink: "New link", group: "Pick an account group", key: "Pick a gateway key", held: "old's key now reaches its group's accounts only" },
  zh: { make: "新建团队", makeThe: "新建", warn: "能用所有账号", noLink: "还没有链接", newLink: "新链接", group: "选择账号组", key: "选择网关密钥", held: "old 的密钥现在只能用它所在组的账号" },
  "zh-TW": { make: "新增團隊", makeThe: "新增", warn: "能用所有帳號", noLink: "還沒有連結", newLink: "新連結", group: "選擇帳號群組", key: "選擇閘道金鑰", held: "old 的金鑰現在只能用它所在群組的帳號" },
  ja: { make: "チームを作成", makeThe: "作成", warn: "すべてのアカウントを使える", noLink: "リンクはまだありません", newLink: "新しいリンク", group: "アカウントグループを選択", key: "ゲートウェイキーを選択", held: "old のキーはグループのアカウントだけを使うようになりました" },
  de: { make: "Team anlegen", makeThe: "Anlegen", warn: "Erreicht jedes Konto", noLink: "Noch kein Link", newLink: "Neuer Link", group: "Kontogruppe wählen", key: "Gateway-Schlüssel wählen", held: "Der Schlüssel von old erreicht jetzt nur noch die Konten seiner Gruppe" },
};

// ops is magpie's side of the page: /ops/teams/*, as internal/gui/ops.go
// answers it, over a list the test can read back
function ops(writes) {
  const list = {
    teams: [
      { id: "ec-team", name: "ec-team", groupId: "g1", groupName: "ec", keyId: "k1", keyName: "ec", linked: true, accounts: 2, held: true },
      { id: "old", name: "old", groupId: "g2", groupName: "legacy", keyId: "k2", keyName: "legacy", linked: false, accounts: 0, held: false },
    ],
    groups: [{ id: "g1", name: "ec", accounts: 2 }, { id: "g2", name: "legacy", accounts: 0 }, { id: "g3", name: "payments", accounts: 3 }],
    keys: [{ id: "k1", name: "ec", issues: true }, { id: "k2", name: "legacy", issues: false }, { id: "k3", name: "payments-key", issues: true }],
  };
  return async (route) => {
    const req = route.request(), what = new URL(req.url()).pathname.replace("/ops/teams/", "");
    const json = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (req.method() === "GET" && what === "list") return json(list);
    const body = JSON.parse(req.postData() || "{}");
    writes.push({ what, body, origin: new URL(req.url()).origin });
    const team = list.teams.find((x) => x.id === body.id);
    switch (what) {
      case "add":
        list.teams.push({ id: body.name, name: body.name, groupId: body.group, groupName: "payments", keyId: body.key, keyName: "payments-key", linked: true, accounts: 3, held: true });
        return json({ id: body.name, name: body.name, link: "/team/tok-made" });
      case "token":
        team.linked = true;
        return json({ id: team.id, name: team.name, link: "/team/tok-new" });
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

        // held with one click
        await row("old").getByRole("button", { name: w.warn }).click();
        await page.waitForFunction(() => !document.querySelector('#tpTeams .acc[data-team="old"] .amodels.set'));
        assert.deepEqual(writes.at(-1), { what: "hold", body: { id: "old" }, origin: "http://magpie.test" });
        assert.equal((await page.locator("#status").textContent()).trim(), w.held);

        // a team made: a name, a group and a key, then its link once
        await page.getByRole("button", { name: w.make, exact: true }).click();
        await editor.waitFor();
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
        assert.equal((await ok().textContent()).trim(), w.makeThe);
        await ok().click();
        await page.waitForFunction(() => document.querySelector("#modal .editor .mono")?.textContent.includes("/team/tok-made"));
        assert.deepEqual(writes.at(-1).body, { name: "payments", group: "g3", key: "k3" });
        assert.equal((await editor.locator(".mono").textContent()).trim(), "http://magpie.test/team/tok-made");
        await ok().click();
        await row("payments").waitFor();

        // New link asks first: the old one stops
        await row("ec-team").getByRole("button", { name: w.newLink, exact: true }).click();
        await accept().waitFor();
        assert.equal(writes.filter((x) => x.what === "token").length, 0, "a new link was made before it was asked");
        await accept().click();
        await page.waitForFunction(() => document.querySelector("#modal .editor .mono")?.textContent.endsWith("/team/tok-new"));
        await ok().click();

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
