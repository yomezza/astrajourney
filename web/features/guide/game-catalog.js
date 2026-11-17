import { objectHologram } from "../objects/presentation.js";
import { I18n } from "../../core/i18n.js";
import { UI } from "../../ui/runtime.js";
import { Game } from "../runtime.js";
import { d, w } from "../selectors.js";
export const objectArticles = [
  [
    "star",
    "Звезда",
    "Звезда",
    "Масса и светимость звезды определяют условия в системе. Спектральный класс описывает температуру её поверхности.",
  ],
  [
    "garden",
    "Садовый мир",
    "Садовый мир",
    "Мир с умеренной температурой и потенциальными источниками биосырья. Условия конкретной планеты определяются разведкой.",
  ],
  [
    "rocky",
    "Каменистая",
    "Каменистая",
    "Твёрдый мир с силикатной корой. Состав месторождений различается между планетами.",
  ],
  [
    "icy",
    "Ледяная",
    "Ледяная",
    "Холодный мир с запасами льда и замёрзших летучих веществ.",
  ],
  [
    "gas",
    "Газовый гигант",
    "Газовый гигант",
    "Крупный мир с протяжённой газовой оболочкой; источник летучих компонентов.",
  ],
  [
    "volcanic",
    "Вулканическая",
    "Вулканическая",
    "Горячая планета с активной поверхностью и минеральными залежами.",
  ],
  [
    "belt",
    "Пояс астероидов",
    "Пояс астероидов",
    "Область малых каменистых тел. Общая квота добычи разделяется всеми игроками.",
  ],
  [
    "ice",
    "Ледяное поле",
    "Ледяное поле",
    "Скопление ледяных тел, используемых для добычи воды и летучих веществ.",
  ],
  [
    "station",
    "Орбитальная станция",
    "Орбитальная станция",
    "Обслуживает корабли и торговлю. Ассортимент, услуги и принадлежность различаются.",
  ],
  [
    "black-hole",
    "Чёрная дыра",
    "Чёрная дыра",
    "Центральный объект Эдема и узел проекта межгалактического перехода.",
  ],
];
export function gameCards() {
  return [
    {
      id: "game:technical:experience",
      name: "Стаж и размер флота",
      category: "game:technical",
      technical: true,
      desc: "",
    },
    ...(d().resources || []).map((r) => ({
      id: "game:resource:" + r.id,
      name: r.name,
      code: r.code,
      desc: r.description,
      resource: r,
      category: "game:resources",
      subcategory: r.kind,
    })),
    ...(w().ship_classes || []).map((ship) => ({
      id: "game:ship:" + ship.id,
      name: ship.name,
      desc: ship.description,
      ship,
      category: "game:ships",
      subcategory: ship.role,
    })),
    ...objectArticles.map(([id, name, kind, desc]) => ({
      id:
        "game:object:" +
        ({ belt: "asteroids", ice: "ice-field", volcanic: "lava" }[id] || id),
      name,
      kind,
      desc,
      category: "game:objects",
    })),
  ];
}
export function renderGameArticle(
  article,
  item,
  { shipStats, filterShipyards, open },
) {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    row = UI.row;
  if (item.technical) {
    article.append(
      n(
        "p",
        tr(
          "Стаж не ограничен сотым уровнем. Лимит флота определяет число принадлежащих игроку кораблей.",
        ),
      ),
      UI.table(
        ["Стаж игрока", "Кораблей"],
        [
          ["1–5", "2"],
          ["6–10", "3"],
          ["11–16", "4"],
          ["17–25", "6"],
          ["26–35", "7"],
          ["36+", "7"],
        ],
      ),
      n(
        "p",
        tr(
          "После стажа 35 сохраняется лимит 7 кораблей до появления новых диапазонов. Имеющиеся корабли не изымаются. Ограничения стажа для покупки конкретной модели действуют отдельно.",
        ),
      ),
    );
  } else if (item.ship) {
    const model = UI.hologram(
      item.ship.model,
      tr(item.ship.name),
      "ship-model",
    );
    article.append(model, n("p", tr(item.desc)));
    shipStats(article, item.ship);
    article.append(
      b("Найти верфь", () => {
        filterShipyards({ classID: item.ship.id, role: "all", query: "" });
        Game.openShipyards();
      }),
    );
  } else if (item.resource) {
    article.append(n("div", item.code, "guide-code"), n("p", tr(item.desc)));
    row(article, "Источники", tr(item.resource.sources));
    row(article, "Редкость", tr(item.resource.rarity));
    row(article, "Применение", item.resource.uses.map(tr).join(" · "));
    article.append(n("h3", tr("Связанные материалы")));
    for (const r of d()
      .resources.filter(
        (r) =>
          r.id !== item.id &&
          r.uses.some((tag) => item.resource.uses.includes(tag)),
      )
      .slice(0, 5))
      article.append(
        b(r.name, () => {
          open("game:resource:" + r.id);
        }),
      );
  } else {
    const canvas = objectHologram({ kind: item.kind });
    article.append(
      canvas,
      n("p", tr(item.desc)),
      n(
        "p",
        tr(
          "Параметры и месторождения конкретных объектов открываются после исследования.",
        ),
        "muted",
      ),
    );
  }
}
