// A gateway key is let issue member keys under itself (issuer-key, what
// `magpie gateway-key issuer <id> on` sets) from its row's menu, under
// Advanced, and a key that may says so in its row. A team portal issues
// its members' keys under such a key. The sharing key, a read-only key and
// Magpie Browser's aren't offered it, as they aren't the management API.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const words = {
  en: { more: "Key actions", adv: "Advanced", head: "Issue member keys", off: "Off", on: "Can issue", tag: "Issues member keys", madeOn: "Team can issue member keys", madeOff: "Team no longer issues member keys" },
  zh: { more: "密钥操作", adv: "高级", head: "签发成员密钥", off: "关闭", on: "可以签发", tag: "可签发成员密钥", madeOn: "Team 现在可以签发成员密钥", madeOff: "Team 不再签发成员密钥" },
  "zh-TW": { more: "金鑰操作", adv: "進階", head: "簽發成員金鑰", off: "關閉", on: "可以簽發", tag: "可簽發成員金鑰", madeOn: "Team 現在可以簽發成員金鑰", madeOff: "Team 不再簽發成員金鑰" },
  ja: { more: "キーの操作", adv: "詳細設定", head: "メンバーキーの発行", off: "オフ", on: "発行できる", tag: "メンバーキーを発行", madeOn: "Team はメンバーキーを発行できます", madeOff: "Team はメンバーキーを発行しなくなりました" },
  de: { more: "Schlüsselaktionen", adv: "Erweitert", head: "Mitgliederschlüssel ausstellen", off: "Aus", on: "Darf ausstellen", tag: "Stellt Mitgliederschlüssel aus", madeOn: "Team kann Mitgliederschlüssel ausstellen", madeOff: "Team stellt keine Mitgliederschlüssel mehr aus" },
};

const keys = () => [
  { id: "lead", name: "Lead", masked: "sk-magpie-key-…000000", issues: true },
  { id: "team", name: "Team", masked: "sk-magpie-key-…222222" },
  { id: "laptop", name: "Laptop", masked: "sk-magpie-key-…111111", readOnly: true },
  { id: "magpie", name: "Magpie", masked: "sk-magpie-key-…444444", lan: true },
  { id: "ext", name: "Magpie Browser", masked: "sk-magpie-key-…555555", readOnly: true, browser: true },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1100, 420]) {
    for (const [lang, w] of Object.entries(words)) {
      test(`${engine} ${width} ${lang}: a gateway key is let issue member keys and has it taken away`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const events = [], errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, "light", events, { lan: true, keys: keys() }));
        await page.goto("http://magpie.test/?view=gateway");
        await page.locator("#gatewayKeys .acc[data-key]").last().waitFor();
        const row = (id) => page.locator(`#gatewayKeys .acc[data-key="${id}"]`);
        const tag = (id) => row(id).locator(".key-issues");
        const item = (name) => page.locator(".row-menu [role=menuitem]").filter({ hasText: name });
        const level = (name) => page.locator(".proto-menu .pm-item").filter({ has: page.locator(".pm-name", { hasText: new RegExp(`^${name}$`) }) });
        const menu = async (id) => {
          await row(id).hover();
          await row(id).getByRole("button", { name: w.more, exact: true }).click();
          await page.locator(".row-menu").waitFor();
        };
        const levels = async (id) => {
          await menu(id);
          await item(w.adv).click();
          await item(w.head).click();
          await page.locator(".proto-menu").waitFor();
          assert.equal((await page.locator(".proto-menu .pm-head").textContent()).trim(), w.head);
        };
        const sets = () => events.filter((e) => e.action === "issuer-key").map((e) => e.body);

        // a key that may issue says so always, not only on hover
        await page.mouse.move(0, 0);
        assert.equal((await tag("lead").textContent()).trim(), w.tag);
        assert.ok((await tag("lead").boundingBox()).width > 0, "the tag is hidden");
        assert.ok((await tag("lead").getAttribute("title")).length > 10, "the tag says what it means");
        assert.equal(await tag("team").count(), 0);

        const view = page.locator("#view-gateway");
        const scrolled = async () => [await view.evaluate((v) => v.scrollTop), await page.evaluate(() => window.scrollY)];
        const before = await scrolled();

        await levels("team");
        assert.equal(await level(w.off).getAttribute("aria-checked"), "true");
        assert.equal(await level(w.on).getAttribute("aria-checked"), "false");
        await level(w.on).click();
        await page.waitForFunction(() => !!document.querySelector('#gatewayKeys .acc[data-key="team"] .key-issues'));
        assert.deepEqual(sets(), [{ key: "team", issues: true }]);
        assert.equal((await page.locator("#status").textContent()).trim(), w.madeOn);

        await levels("team");
        assert.equal(await level(w.on).getAttribute("aria-checked"), "true");
        await level(w.off).click();
        await page.waitForFunction(() => !document.querySelector('#gatewayKeys .acc[data-key="team"] .key-issues'));
        assert.deepEqual(sets().at(-1), { key: "team", issues: false });
        assert.equal((await page.locator("#status").textContent()).trim(), w.madeOff);

        // picking what it has already changes nothing
        await levels("lead");
        assert.equal(await level(w.on).getAttribute("aria-checked"), "true");
        await level(w.on).click();
        assert.equal(sets().length, 2);

        // keys that can't issue aren't offered it
        for (const id of ["laptop", "magpie", "ext"]) {
          await menu(id);
          assert.equal(await item(w.adv).count(), 0, `${id} offers Advanced`);
          await page.keyboard.press("Escape");
          assert.equal(await tag(id).count(), 0);
        }
        assert.deepEqual(await scrolled(), before, "a click moved the page");

        const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert.ok(over <= 0, `the page scrolls sideways by ${over}px`);
        const rowBox = await row("lead").boundingBox(), b = await tag("lead").boundingBox();
        assert.ok(b.x >= rowBox.x && b.x + b.width <= rowBox.x + rowBox.width + 1, "the tag runs out of its row");
        assert.equal(await page.locator("select").count(), 0, "no native select");
        assert.deepEqual(errors, []);
      });
    }
  }
}
