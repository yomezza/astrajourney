import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

async function station(t, runTrade) {
  const id = "42000000-0000-4000-8000-000000000001";
  const layout = {version:1,active:id,default:id,workspaces:[{
    id,name:"Командование",ship_id:101,customized:true,modules:[{
      id:"42000000-0000-4000-8000-000000000002",type:"market",binding:"fixed",ship_id:102,view:"card",
    }],
  }]};
  const g = await game(t,{signedIn:true,faults:[
    ["workspaces",({json}) => json({revision:1,layout})],
    ["expedition",request => {
      const {req,data,url,json} = request;
      if (req.method() === "POST") return runTrade(request);
      const id = Number(url.searchParams.get("ship"));
      const ship = id ? data.fleet.ships.find(ship => ship.id === id) : data.expedition.ship;
      return json({...data.expedition,ship});
    }],
  ]});
  const market = g.page.locator('.matrix-card[data-module="market"]');
  const confirm = g.page.locator("#window-confirm");
  await g.ready();
  await market.getByRole("button",{name:/Железо/}).click();
  await market.getByRole("spinbutton",{name:"Количество"}).fill("2");
  const purchase = market.getByRole("button",{name:"Купить в трюм"});
  return {...g,market,confirm,purchase,async buy() {
    await purchase.click();
    await confirm.getByText(/Test vessel 2 #102/).waitFor();
    await confirm.getByRole("button",{name:"Подтвердить",exact:true}).click();
    await confirm.waitFor({state:"hidden"});
  }};
}

function receipt(command) {
  return {request_id:command.request_id,ship:command.ship,action:command.action};
}

test("Studio recovers a committed trade after the response and page are lost", async t => {
  let command;
  const g = await station(t,({req,route}) => {
    command = req.postDataJSON();
    return route.abort("connectionreset");
  });
  await g.buy();
  await g.market.getByText("Результат сделки пока неизвестен",{exact:true}).waitFor();
  assert.equal(await g.purchase.isEnabled(),false);
  g.handlers.set("expedition/commands/" + command.request_id,({json}) => json(receipt(command)));
  await g.page.reload();
  await g.ready();
  await g.market.getByRole("button",{name:"Проверить сделку",exact:true}).click();
  await g.market.getByRole("button",{name:"Проверить сделку",exact:true}).waitFor({state:"hidden"});
  assert.equal(g.requests.filter(r => r.method === "POST" && r.path === "expedition").length,1);
  assert.equal(command.ship,102);
  assert.equal(command.expected_object,"port-1");
});

test("Studio repeats only the same uncertain trade when its receipt is absent", async t => {
  const commands = [];
  const g = await station(t,({req,route,json}) => {
    const command = req.postDataJSON();
    commands.push(command);
    return commands.length === 1 ? route.abort("connectionreset") : json(receipt(command));
  });
  await g.buy();
  g.handlers.set("expedition/commands/" + commands[0].request_id,({json}) =>
    json({code:"ACTION_UNCONFIRMED",error:"Подтверждение сделки пока не найдено"},404));
  const check = g.market.getByRole("button",{name:"Проверить сделку",exact:true});
  await check.click();
  await g.page.getByText("Подтверждение сделки пока не найдено",{exact:true}).waitFor();
  assert.equal(await g.purchase.isEnabled(),false);
  const retry = g.market.getByRole("button",{name:"Повторить тот же запрос",exact:true});
  await retry.click();
  await retry.waitFor({state:"hidden"});
  assert.equal(commands.length,2);
  assert.deepEqual(commands[1],commands[0]);
});

test("Studio preserves the quantity and requires a new confirmation after a price conflict", async t => {
  const commands = [];
  const g = await station(t,({req,data,json}) => {
    const command = req.postDataJSON();
    commands.push(command);
    if (commands.length === 1) {
      data.expedition.offers[0].buy = 7;
      return json({code:"MARKET_PRICE_CHANGED",error:"Цена изменилась. Проверьте предложение ещё раз"},409);
    }
    return json(receipt(command));
  });
  await g.buy();
  await g.market.getByText("7 CR",{exact:true}).waitFor();
  assert.equal(await g.market.getByRole("spinbutton",{name:"Количество"}).inputValue(),"2");
  assert.equal(commands.length,1);
  await g.buy();
  assert.equal(commands.length,2);
  assert.equal(commands[0].expected_price,5);
  assert.equal(commands[1].expected_price,7);
  assert.notEqual(commands[0].request_id,commands[1].request_id);
});
