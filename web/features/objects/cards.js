import { openPlanet } from "../planet/loader.js";
import { objectModel, objectHologram, objectTag } from "./presentation.js";
import { stationLabel, stationEditor } from "../stations/index.js";
import { registerTooltip } from "../../ui/tooltip.js";

import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { UI, Modules } from "../../ui/runtime.js";
import { Game } from "../../features/runtime.js";
import { world } from "../../core/world-store.js";
import { node as createNode } from "../../ui/dom.js";

const tr = (s) => I18n.t(s),
  node = createNode;
const objects = new Map();
function renderObject(container, site, star) {
  const scroll = container.scrollTop;
  let model = container.querySelector(":scope > .object-hologram"),
    root = container.querySelector(":scope > .object-information");
  if (!model) {
    container.replaceChildren();
    model = objectHologram(site, star);
    root = node("div", undefined, "object-information");
    container.append(model, root);
  }
  const asset = objectModel(site, star);
  UI.updateHologram(model, asset, tr(site.kind));
  const title = node("h2");
  title.append(
    site.kind === "Орбитальная станция"
      ? stationLabel(star.id, site.id, site.name)
      : document.createTextNode(site.name),
  );
  root.replaceChildren(title, objectTag(site.kind));
  if (site.details) {
    const p = site.details;
    const params = node("p");
    for (const [value, message] of [
      [
        p.mass + " M⊕",
        "M⊕ — масса Земли. Значение 1 означает массу, равную земной.",
      ],
      [
        p.gravity + " g",
        "g — земная сила тяжести. 1 g примерно равно 9,81 м/с².",
      ],
      [
        p.temperature + " K",
        "K — кельвины. Для приблизительного перевода в °C вычтите 273. Температура равновесная, без парникового эффекта.",
      ],
      [
        p.au + " AU",
        "AU — астрономическая единица, среднее расстояние от Земли до Солнца: около 149,6 млн км.",
      ],
    ]) {
      const span = node("span", value, "help");
      span.dataset.help = message;
      span.tabIndex = 0;
      params.append(span, document.createTextNode(" · "));
    }
    root.append(params, node("p", tr(p.atmosphere)));
  }
  if (site.shared_quota) {
    const p = node(
        "p",
        tr("Общий запас") +
          `: ${site.shared_quota.remaining} / ${site.shared_quota.capacity}`,
      ),
      time = node("span");
    time.dataset.resetAt = site.shared_quota.reset_at;
    p.append(document.createTextNode(" · "), time);
    root.append(p);
  }
  root.append(
    node(
      "h3",
      tr(site.kind === "Орбитальная станция" ? "Услуги порта" : "Ресурсы"),
    ),
  );
  if (site.resources?.length)
    for (const id of site.resources) {
      const r = Session.data.expedition.resources.find((r) => r.id === id),
        p = node("p", tr(r?.name || id));
      if (site.quotas?.[id])
        p.append(
          document.createTextNode(
            ` · ${site.quotas[id].remaining} / ${site.quotas[id].capacity}`,
          ),
        );
      root.append(p);
    }
  else
    root.append(
      node(
        "p",
        tr(
          site.kind === "Орбитальная станция"
            ? "У каждого порта свой ассортимент и склад. Для торговли и получения груза нужна стыковка с выбранной станцией."
            : "Месторождения откроются после разведки на месте.",
        ),
      ),
    );
  if (site.quality) {
    UI.row(root, "Качество", site.quality);
    UI.row(root, "Концентрация", site.concentration + "%");
    const help = node(
      "p",
      tr(
        "Качество Q1–Q5 и концентрация влияют на производительность специализированного добытчика.",
      ),
      "muted",
    );
    root.append(help);
  }
  if (site.kind === "Орбитальная станция") {
    const port = Session.data.workbench?.stations.find(
      (p) => p.star === star.id && p.object_id === site.id,
    );
    if (port) {
      UI.row(root, "Специализация", tr(port.specialization));
      UI.row(
        root,
        "Владелец",
        port.owner_name
          ? port.owner_kind === "founder"
            ? tr(port.owner_name)
            : port.owner_name
          : tr("Нейтральная станция"),
      );
      UI.row(root, "Тариф", port.fee_percent + "%");
      UI.row(root, "Репутация", port.reputation);
      if (port.shipyard)
        root.append(
          UI.button("Открыть верфь", () =>
            Game.openShipyards(star.id, site.id),
          ),
        );
      root.append(
        node(
          "p",
          tr(
            "Репутация 20 отменяет тариф корпорации-основателя 02. Тариф округляется вверх до целого CR.",
          ),
          "muted",
        ),
      );
    }
  }
  const guide = node("button", tr("Руководство пилота"));
  guide.onclick = () =>
    Game.openGuide(
      site.details
        ? {
            Ледяная: "icy",
            "Садовый мир": "garden",
            "Газовый гигант": "gas",
            Вулканическая: "volcanic",
          }[site.kind] || "rocky"
        : site.kind === "Орбитальная станция"
          ? "station"
          : site.kind === "Ледяное поле"
            ? "ice"
            : "belt",
    );
  root.append(guide);
  const saved = Session.data.workbench?.notes.find(
    (item) => item.star === star.id && item.object_id === site.id,
  );
  const note = UI.field(
    root,
    "Личная заметка",
    "object-note-" + star.id + "-" + site.id,
    saved?.note || "",
  );
  const actions = node("div", undefined, "object-footer-actions");
  actions.append(
    UI.button("Сохранить заметку", () =>
      Game.command({
        action: "note",
        star: star.id,
        target: site.id,
        note: note.value,
        bookmark: false,
      }),
    ),
  );
  if (site.kind === "Орбитальная станция")
    actions.append(
      UI.button("Личное название и заметка", () =>
        stationEditor(star.id, site.id),
      ),
    );
  if (site.details)
    actions.append(
      UI.button("Поверхность планеты", () =>
        openPlanet(star.id, site.details.id),
      ),
    );
  root.append(actions);
  container.scrollTop = scroll;
}
function resourceCard(r) {
  const box = node("div", undefined, "resource-card");
  box.append(
    node("span", r.code, "resource-code"),
    node("h3", tr(r.name)),
    node("p", tr(r.description)),
    node("p", tr(r.sources), "muted"),
    node("p", tr(r.origin), "muted"),
  );
  if (r.id === "agni")
    box.append(
      node("p", tr("1 ед. водорода = до 5 единиц топлива"), "resource-fuel"),
    );
  const tags = node("div", undefined, "resource-tags");
  for (const text of [r.rarity, ...(r.uses || [])])
    tags.append(node("span", tr(text)));
  box.append(tags);
  return box;
}
export function showResource(id) {
  const r = Session.data.expedition?.resources.find((r) => r.id === id);
  if (!r) return;
  const root = Modules.create(
    "resource-" + id,
    "Информация о ресурсе",
    500,
    500,
    { transient: true },
  );
  root.replaceChildren(resourceCard(r));
  Modules.open("resource-" + id);
}
export function showObject(site, star) {
  const key = "object-" + star.id + "-" + site.id,
    root = Modules.create(key, "Информация об объекте", 600, 650, {
      transient: true,
    });
  objects.set(key, { root, site, star });
  renderObject(root, site, star);
  Modules.open(key);
}
export function initObjectCards() {
  window.addEventListener("gamedata", async () => {
    const epoch = Session.epoch,
      cache = new Map();
    for (const [key, entry] of objects) {
      if (world.data?.stars[entry.star.id - 1]?.deleted) {
        Modules.close(key);
        objects.delete(key);
        continue;
      }
      if (
        !Modules.isOpen(key) ||
        (entry.root.contains(document.activeElement) &&
          document.activeElement.matches("input,textarea"))
      )
        continue;
      if (!cache.has(entry.star.id))
        cache.set(entry.star.id, api("system?id=" + entry.star.id));
      try {
        const data = await cache.get(entry.star.id);
        if (epoch !== Session.epoch) return;
        if (
          !Modules.isOpen(key) ||
          objects.get(key) !== entry ||
          !entry.root.isConnected
        )
          continue;
        const site = data.sites.find((s) => s.id === entry.site.id);
        if (site) renderObject(entry.root, site, data.star);
      } catch {}
    }
  });
  window.addEventListener("signedout", () => {
    objects.clear();
  });
  registerTooltip("[data-resource]", (target) => {
    const resource = Session.data.expedition?.resources.find(
      (item) => item.id === target.dataset.resource,
    );
    return resource ? resourceCard(resource) : undefined;
  });
  window.addEventListener("moduledestroy", ({ detail }) =>
    objects.delete(detail),
  );
}
