import assert from "node:assert/strict";
import { chromium } from "playwright";

// Only run against a disposable, nonce-marked integration server.
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.equal(base.hostname, "127.0.0.1");
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE
);
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH || undefined,
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
async function login(page, name) {
  await page.locator('#auth-form input[name="username"]').fill(name);
  await page.locator('#auth-form input[name="password"]').fill("1");
  await page.locator("#submit").click();
  await page.locator("#studio:not([hidden])").waitFor();
  await page.locator("#studio .studio-tabs").waitFor({state:"attached"});
}
const button = (page, name) => page.locator("#studio").getByRole("button", {name, exact:true});
const saved = (page) => page.locator("#studio").getByText("Раскладка сохранена").waitFor();
const cargo = "#window-studio-edec00a2-0000-4000-8000-000000000001";
try {
  const first = await browser.newContext({viewport:{width:1440,height:1000}});
  const second = await browser.newContext({viewport:{width:1440,height:1000}});
  const a = await first.newPage(), b = await second.newPage();
  const errors = [], commands = [];
  for (const page of [a,b]) {
    page.setDefaultTimeout(10000);
    page.on("pageerror", error => errors.push(error.message));
    page.on("request", request => {
      const path = new URL(request.url()).pathname;
      if (request.method() === "POST" && !["/api/workspaces","/api/login","/api/logout"].includes(path))
        commands.push(path);
    });
    await page.goto(base.href);
  }
  await login(a,"studio_browser");
  await login(b,"studio_browser");
  await button(a,"Second watch").click();
  await saved(a);
  await a.locator(cargo).waitFor();
  assert.equal(await a.locator(cargo+" dd").textContent(),"19");
  assert.equal((await a.locator(cargo).boundingBox()).width,395);

  await button(b,"Retired ship").click();
  await b.locator("#studio").getByText("Рабочие пространства изменились в другой вкладке").waitFor();
  await button(b,"Загрузить сохранённую раскладку").click();
  await b.locator(cargo+" dd").getByText("19",{exact:true}).waitFor();
  await button(b,"Retired ship").click();
  await b.locator(".studio-view").getByText(/Корабль недоступен/).waitFor();
  await saved(b);

  await b.locator("#profile-menu > summary").click();
  await b.locator("#logout").click();
  await b.locator("#auth:not([hidden])").waitFor();
  assert.equal(await b.locator(".studio-window").count(),0);
  await login(b,"studio_peer");
  assert.equal(await button(b,"Retired ship").count(),0);
  assert.deepEqual(await b.locator("#studio .studio-tabs button").allTextContents(), ["Командование","Добыча","Торговля"]);

  const fresh = await browser.newPage({viewport:{width:320,height:720}});
  fresh.setDefaultTimeout(10000);
  await fresh.goto(base.href);
  await login(fresh,"studio_browser");
  await fresh.locator(".studio-view").getByText(/Корабль недоступен/).waitFor();
  await fresh.locator("#matrix-mobile [data-studio]").focus();
  await fresh.keyboard.press("Enter");
  const select = fresh.locator("#studio").getByLabel("Рабочее пространство",{exact:true});
  assert.equal(await select.evaluate(el => el === document.activeElement),true);
  await select.selectOption({label:"Second watch"});
  await fresh.locator(cargo+" dd").getByText("19",{exact:true}).waitFor();
  assert.ok((await fresh.locator(cargo).boundingBox()).width <= 320);
  await saved(fresh);
  assert.deepEqual(commands,[]);
  assert.deepEqual(errors,[]);
  console.log("Real Web/Cosmic/PostgreSQL: restore, conflict, revoked ship, account isolation and 320px keyboard navigation passed.");
} finally {
  await browser.close();
}
