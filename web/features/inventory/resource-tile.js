import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
import { Format } from "../../core/format.js";
export function resourceTile(resource, id, quantity, activate) {
  const tile = UI.button("", (event) => activate(event, tile));
  tile.className =
    "cargo-tile" + (resource?.rarity === "Редкий" ? " rare" : "");
  tile.dataset.resource = id;
  tile.append(
    UI.node("small", I18n.t(resource?.kind || "Ресурс")),
    UI.node("b", resource?.code || id),
    UI.node("span", Format.quantity(quantity)),
  );
  return tile;
}
