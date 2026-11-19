import { api } from '../../core/api.js';
import { Session } from '../../core/session.js';
import { UI } from '../../ui/runtime.js';
import { I18n } from '../../core/i18n.js';
import { Format } from '../../core/format.js';
import { moduleCode, moduleEffects } from '../outfitting/index.js';

export function createModulesMarket({ state, command, renderMarket }) {
  const views = new WeakMap();
  const n = (...args) => UI.node(...args), tr = text => I18n.t(text);
  function createView(root) {
    const view = { cards: new Map(), stationsKey: '', data: null };
    view.station = UI.choice(root, 'Станция', [], '', value => {
      state.moduleLocation = value;
      return renderMarket();
    });
    root.append(n('p', tr('Ассортимент и классы отличаются между станциями. Покупка доступна только на станции продавца.'), 'muted'));
    const filters = n('div', undefined, 'inline-controls');
    view.kind = UI.choice(filters, 'Тип модуля', [['', 'Все типы'], ['extractor', 'Добытчики'], ['cargo', 'Трюм'], ['scanner', 'Сканер'], ['scoop', 'Топливозаборник'], ['drive', 'Ускоритель перехода']], state.moduleKind || '', value => {
      state.moduleKind = value;
      return renderMarket();
    });
    view.rating = UI.choice(filters, 'Класс', [['', 'Все классы'], ...['S', 'A', 'B', 'C'].map(r => [r, r])], state.moduleRating || '', value => {
      state.moduleRating = value;
      return renderMarket();
    });
    view.grid = n('div', undefined, 'equipment-market');
    root.append(filters, view.grid);
    views.set(root, view);
    return view;
  }
  function createCard(view, module) {
    const card = n('article', undefined, 'equipment-offer');
    const code = n('strong', '', 'equipment-code'), title = n('h3'), effects = n('p'), description = n('small'), stock = n('p');
    const entry = { card, code, title, effects, description, stock, module };
    entry.buy = UI.button('Купить модуль', () => {
      const data = view.data, m = entry.module;
      return command({ action: 'module_buy', ship: data.ship.id, target: m.catalog_id, price: m.price, star: data.star, station: data.station });
    });
    card.append(code, title, effects, description, stock, entry.buy);
    return entry;
  }
  function text(node, value) {
    if (node.textContent !== value) node.textContent = value;
  }
  function choice(picker, value) {
    if (picker.value !== value) picker.setValue(value);
  }
  return async function renderModules(root, lease) {
    const sid = Session.data.expedition.ship.id, epoch = Session.epoch;
    const location = state.moduleLocation || '';
    const query = new URLSearchParams({ ship: String(sid) });
    if (location) {
      const [star, ...parts] = location.split('|');
      query.set('star', star);
      query.set('station', parts.join('|'));
    }
    const data = await api('ship/modules?' + query);
    if (epoch !== Session.epoch || state.tab !== 'modules' || (lease && !lease())) return false;
    // Keep controls and cards alive while fetching and patch only changed values.
    const view = views.get(root) || createView(root);
    view.data = data;
    const here = data.stations.find(p => p.star === data.star && p.object_id === data.station);
    const current = here ? `${data.star}|${data.station}` : '';
    const choices = [['', 'Выберите станцию'], ...data.stations.map(p => [`${p.star}|${p.object_id}`, p.name])];
    const stationsKey = JSON.stringify(choices);
    if (view.stationsKey !== stationsKey) {
      view.station.setOptions(choices);
      view.stationsKey = stationsKey;
    }
    choice(view.station, current);
    choice(view.kind, state.moduleKind || '');
    choice(view.rating, state.moduleRating || '');
    const offers = (data.offers || []).filter(m => (!state.moduleKind || m.kind === state.moduleKind) && (!state.moduleRating || m.rating === state.moduleRating));
    const visible = new Set(offers.map(m => m.catalog_id));
    for (const [id, entry] of view.cards) {
      if (!visible.has(id)) { entry.card.remove(); view.cards.delete(id); }
    }
    if (offers.length) { view.empty?.remove(); view.empty = null; }
    for (const [index, m] of offers.entries()) {
      const entry = view.cards.get(m.catalog_id) || createCard(view, m);
      view.cards.set(m.catalog_id, entry);
      entry.module = m;
      if (entry.card.dataset.rating !== m.rating) entry.card.dataset.rating = m.rating;
      text(entry.code, moduleCode(m));
      text(entry.title, tr(m.name));
      text(entry.effects, moduleEffects(m));
      text(entry.description, tr(m.description));
      text(entry.stock, `${Format.credits(m.price)} · ${tr('В наличии')}: ${m.quantity}`);
      const canBuy = data.docked && data.ship.star === data.star && data.ship.object_id === data.station && m.quantity > 0 && data.cargo_used + m.size <= data.ship.spec.cargo && m.price <= Session.data.expedition.credits;
      entry.buy.disabled = !canBuy;
      if (view.grid.children[index] !== entry.card) view.grid.insertBefore(entry.card, view.grid.children[index] || null);
    }
    if (!offers.length) {
      if (!view.empty) { view.empty = n('p', '', 'empty-state'); view.grid.append(view.empty); }
      text(view.empty, tr(here ? 'Модулей в продаже нет' : 'Выберите станцию'));
    }
    return true;
  };
}
