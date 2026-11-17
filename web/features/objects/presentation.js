import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export const objectModel = (site, star) => {
  const types = {
    Ледяная: "icy",
    Каменистая: "rocky",
    "Садовый мир": "garden",
    "Газовый гигант": "gas",
    Пустынная: "desert",
    Лавовая: "lava",
    Вулканическая: "lava",
    "Орбитальная станция": "station",
    "Пояс астероидов": "asteroids",
    "Ледяное поле": "ice-field",
    "Чёрная дыра": "black-hole",
  };
  let type = types[site.kind] || "rocky";
  if (site.kind === "Звезда")
    type =
      "star-" +
      (["O", "B", "A", "F", "G", "K", "M"].includes(star?.class)
        ? star.class.toLowerCase()
        : "g");
  return "object-" + type + ".svg";
};
export const objectHologram = (site, star) => {
  return UI.hologram(objectModel(site, star), tr(site.kind), "object-hologram");
};
export const objectTag = (kind) => {
  const colors = {
    "Орбитальная станция": "#7cc8d2",
    "Садовый мир": "#8bcf9e",
    Ледяная: "#8ec6ee",
    Каменистая: "#b0b1b3",
    "Газовый гигант": "#d9b889",
    Пустынная: "#d7bb83",
    Лавовая: "#e58c72",
    Вулканическая: "#e58c72",
    "Пояс астероидов": "#bfb29a",
    "Ледяное поле": "#9fd7eb",
    Звезда: "#efbd68",
    "Чёрная дыра": "#b59ad4",
  };
  const tag = n("span", tr(kind), "object-kind-tag");
  tag.dataset.kind = kind;
  tag.style.color = colors[kind] || "#b4c4d0";
  return tag;
};
