// Real Chromium + Go handlers + a nonce-marked disposable PostgreSQL installation.
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {chromium} from "playwright";
const fixture=JSON.parse(readFileSync(0,"utf8")),base=new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1","localhost","[::1]"].includes(base.hostname));
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
assert.equal((await fetch(base)).headers.get("x-cosmos-test-run"),process.env.TERMINAL_TEST_NONCE);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_EXECUTABLE_PATH,args:process.env.CHROMIUM_NO_SANDBOX==="1"?["--no-sandbox"]:[]});
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addCookies([{name:fixture.cookie_name,value:fixture.cookie,url:base.href}]);
 const page=await context.newPage();page.setDefaultTimeout(15000);const errors=[];page.on("pageerror",e=>errors.push(e.message));
 const openStudies=async()=>{
  await page.evaluate(async()=>(await import("/bootstrap.js")).application.Modules.open("studies"));
  await page.locator(`#window-studies .study-entry[data-star="${fixture.star}"]`).first().click();
 };
 await page.goto(base.href);await page.locator("#game:not([hidden])").waitFor();await openStudies();
 const studies=page.locator("#window-studies");
 await studies.getByRole("button",{name:"Переименовать систему",exact:true}).click();
 await page.locator("#discovery-new-name").fill("Browser System 280");
 await page.locator("#window-discovery-rename").getByRole("button",{name:"Сохранить",exact:true}).click();
 await page.waitForFunction(()=>!document.querySelector("#window-discovery-rename")?.open);
 await studies.locator(".discovery-record strong").filter({hasText:"Browser System 280"}).waitFor();
 assert.equal(await studies.getByRole("button",{name:"Переименовать систему",exact:true}).count(),0);
 await page.reload();await page.locator("#game:not([hidden])").waitFor();await openStudies();
 await studies.locator(".discovery-record strong").filter({hasText:"Browser System 280"}).waitFor();
 assert.equal(await studies.getByRole("button",{name:"Переименовать систему",exact:true}).count(),0);
 await studies.locator(".studies-current").getByRole("button",{name:"Подробнее",exact:true}).click();
 const objects=page.locator("#window-system-objects");await objects.locator(".research-system-object").first().waitFor();
 const planet=objects.locator(`[data-object-id="planet-${fixture.planet}"] button`);
 await planet.scrollIntoViewIfNeeded();await planet.hover();await page.locator(".research-system-tooltip:visible").waitFor();
 assert.match(await page.locator(".research-system-tooltip:visible").textContent(),/Гравитация/);
 await planet.click();await page.getByRole("menuitem",{name:"Переименовать объект",exact:true}).click();
 await page.locator("#discovery-new-name").fill("Browser Planet 280");
 await page.locator("#window-discovery-rename").getByRole("button",{name:"Сохранить",exact:true}).click();
 await objects.locator(".research-system-object-name").filter({hasText:"Browser Planet 280"}).waitFor();
 await planet.click();assert.equal(await page.getByRole("menuitem",{name:"Переименовать объект",exact:true}).isDisabled(),true);
 await page.keyboard.press("Escape");
 await objects.locator('[data-object-id="asteroids"] button').click();
 assert.equal(await page.getByRole("menuitem",{name:"Переименовать объект",exact:true}).count(),0);
 const payload={operation:crypto.randomUUID(),object:{kind:"planet",uuid:fixture.planet_uuid,star_id:fixture.star,planet_id:fixture.planet},name:"Retry Planet"};
 assert.equal((await page.request.post(new URL("/api/discovery/name",base).href,{data:payload})).status(),409);
 const peer=await browser.newContext();await peer.addCookies([{name:fixture.cookie_name,value:fixture.peer_cookie,url:base.href}]);
 assert.equal((await peer.request.post(new URL("/api/discovery/name",base).href,{data:{...payload,operation:crypto.randomUUID()}})).status(),403);
 assert.deepEqual(errors,[]);await peer.close();await context.close();
 console.log("PASS real API/browser: system button absent after rename/reload, separate objects window, preserved characteristics, one-time planet menu, fields cannot rename, 409/403, no JS errors");
}finally{await browser.close()}
