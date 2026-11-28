import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("Workspace batch transfer retries the exact request after reload", async t => {
  const id = "42000000-0000-4000-8000-000000000001";
  const layout = {version:1,active:id,default:id,workspaces:[{
    id,name:"Командование",ship_id:102,customized:true,modules:[{
      id:"42000000-0000-4000-8000-000000000002",type:"logistics",binding:"fixed",ship_id:101,view:"card",
    }],
  }]};
  const commands = [];
  const g = await game(t,{signedIn:true,faults:[
    ["workspaces",({json}) => json({revision:1,layout})],
    ["logistics",({req,route,json}) => {
      commands.push(req.postDataJSON());
      return commands.length === 1 ? route.abort("connectionreset") : json({mode:"direct"});
    }],
  ]});
  const {page} = g;
  await g.ready();
  const panel = page.locator('.matrix-card[data-module="logistics"]');
  const transfer = panel.getByRole("button",{name:"Переместить ресурсы",exact:true});
  await transfer.click();
  const confirm = page.locator("#window-confirm");
  await confirm.getByLabel("Назначение",{exact:true}).selectOption("102");
  await confirm.getByRole("spinbutton",{name:/Железо/}).fill("2");
  await confirm.getByRole("spinbutton",{name:/Водород/}).fill("1");
  await confirm.getByRole("button",{name:"Подтвердить",exact:true}).click();
  await confirm.waitFor({state:"hidden"});
  await page.reload();
  await g.ready();
  const retry = panel.getByRole("button",{name:"Повторить тот же запрос",exact:true});
  await retry.waitFor();
  assert.equal(await transfer.isEnabled(),false);
  await retry.click();
  await retry.waitFor({state:"hidden"});
  assert.equal(commands.length,2);
  assert.deepEqual(commands[0],commands[1]);
  assert.deepEqual(commands[0].from,{kind:"ship",ship:101});
  assert.deepEqual(commands[0].to,{kind:"ship",ship:102});
  assert.deepEqual(commands[0].items,[{resource:"iron",quantity:2},{resource:"agni",quantity:1}]);
});
