import assert from 'node:assert/strict';
import { test } from 'node:test';
import { game } from './harness.mjs';

test('module market keeps its controls and offers during background refresh', async t => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  const ship = g.data.expedition.ship;
  const response = {
    ship, docked: true, cargo_used: 10,
    stations: [{ star: ship.star, object_id: ship.object_id, name: 'Test Port' }],
    star: ship.star, station: ship.object_id,
    offers: [{ catalog_id: 'scanner-B', name: 'Системный сканер', kind: 'scanner', rating: 'B',
      size: 2, price: 6000, quantity: 4, effects: { scan_percent: 20 }, description: '' }],
  };
  let release, started;
  let hold = false;
  let gate, requested;
  g.handlers.set('ship/modules', async ({ json }) => {
    if (hold) { started(); await gate; }
    return json(response);
  });
  await g.open('market');
  const market = g.page.locator('#window-market');
  await market.getByRole('tab', { name: 'Модули корабля', exact: true }).click();
  await market.locator('.equipment-offer').waitFor();
  await g.page.evaluate(() => {
    const root = document.querySelector('#window-market .module-content');
    window.marketControls = [...root.querySelectorAll('.picker-button')];
    window.marketOffer = root.querySelector('.equipment-offer');
    const { Modules } = window.__testApp;
    const update = Modules.update.bind(Modules);
    Modules.update = (id, work) => {
      const pending = update(id, work);
      if (id === 'market') { window.marketWork = work; window.marketPending = pending; }
      return pending;
    };
    window.marketPoll = () => {
      if (window.marketWork) return update('market', window.marketWork);
      window.dispatchEvent(new Event('gamedata'));
      return window.marketPending;
    };
  });
  // Delay a background poll: the current interface must remain visible.
  hold = true;
  gate = new Promise(resolve => { release = resolve; });
  requested = new Promise(resolve => { started = resolve; });
  t.after(() => release?.());
  await g.page.evaluate(() => { window.marketPending = window.marketPoll(); });
  await requested;
  assert.equal(await market.locator('.equipment-offer').count(), 1, 'pending poll cleared the offers');
  release();
  await g.page.evaluate(() => window.marketPending);
  assert.equal(await g.page.evaluate(() => window.marketOffer === document.querySelector('#window-market .equipment-offer')), true);
  // Changed stock updates the existing card without replacing controls or moving focus.
  hold = false;
  response.offers[0].quantity = 0;
  await g.page.evaluate(() => {
    window.marketControls[1].focus();
    return window.marketPoll();
  });
  assert.equal(await market.getByRole('button', { name: 'Купить модуль', exact: true }).isDisabled(), true);
  assert.match(await market.locator('.equipment-offer').textContent(), /В наличии: 0/);
  assert.equal(await g.page.evaluate(() => window.marketControls.every((node, i) => node === document.querySelectorAll('#window-market .picker-button')[i]) && document.activeElement === window.marketControls[1]), true);
  // Changing a filter must still change the visible assortment.
  await market.locator('[data-field="Класс"]').click();
  await g.page.getByRole('option', { name: 'S', exact: true }).click();
  await market.locator('.equipment-market .empty-state').waitFor();
  assert.equal(await market.locator('.equipment-offer').count(), 0);
});
