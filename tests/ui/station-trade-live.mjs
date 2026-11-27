import assert from "node:assert/strict";
import { mkdir, readFile } from "node:fs/promises";
import { chromium } from "playwright";

const f = JSON.parse(await readFile("/dev/stdin","utf-8"));
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1","localhost","[::1]"].includes(base.hostname));
assert.equal((await fetch(base)).headers.get("x-cosmos-test-run"),process.env.TERMINAL_TEST_NONCE);
const browser = await chromium.launch({
  executablePath:process.env.CHROMIUM_EXECUTABLE_PATH,
  args:process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
try {
  const context = await browser.newContext({viewport:{width:1440,height:900}});
  await context.addCookies([{name:f.cookie_name,value:f.cookie,url:base.href}]);
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  const failures = [];
  page.on("pageerror",error => failures.push(error.message));
  let committed;
  await page.route(url => url.origin === base.origin && url.pathname === "/api/expedition",async route => {
    if (route.request().method() !== "POST") return route.continue();
    const request = route.request().postDataJSON();
    try {
      const response = await route.fetch();
      assert.equal(response.status(),200,await response.text());
      assert.equal((await response.json()).request_id,request.request_id);
      committed = request;
    } catch (error) {
      failures.push(error.message);
    }
    await route.abort("connectionreset");
  });
  await page.goto(base.href);
  const market = page.locator('#studio .matrix-card[data-module="market"]');
  await page.getByRole("combobox",{name:"Корабль пространства",exact:true}).selectOption(String(f.trader));
  await market.getByRole("button",{name:/Водород/}).first().click();
  await market.getByRole("spinbutton",{name:"Количество",exact:true}).fill("2");
  await market.getByRole("button",{name:"Купить в трюм",exact:true}).click();
  const confirm = page.locator("#window-confirm");
  await confirm.getByText(/Trade courier/).waitFor();
  await confirm.getByRole("button",{name:"Подтвердить",exact:true}).click();
  await confirm.waitFor({state:"hidden"});
  await market.getByText("Результат сделки пока неизвестен",{exact:true}).waitFor();
  assert.ok(committed,failures.join("\n"));
  const output = new URL("./artifacts/unified-terminal/",import.meta.url);
  await mkdir(output,{recursive:true});
  await page.screenshot({path:new URL("trade-recovery-desktop.png",output).pathname});
  await page.reload();
  await page.setViewportSize({width:390,height:844});
  const check = market.getByRole("button",{name:"Проверить сделку",exact:true});
  await check.waitFor();
  await check.evaluate(element => element.scrollIntoView({block:"center"}));
  await page.screenshot({path:new URL("trade-recovery-mobile.png",output).pathname});
  await check.click();
  await check.waitFor({state:"hidden"});
  const active = await page.request.get(new URL("api/expedition",base).href);
  assert.equal((await active.json()).ship.id,f.active);
  assert.equal(committed.ship,f.trader);
  assert.deepEqual(failures,[]);
  console.log("One real purchase committed. The response was lost and its receipt restored after reload.");
} finally {
  await browser.close();
}
