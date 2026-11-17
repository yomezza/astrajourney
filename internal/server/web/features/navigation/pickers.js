import { known } from "../selectors.js";

import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
import { Session } from "../../core/session.js";

import { Game } from "../runtime.js";
import { world } from "../../core/world-store.js";
import { refreshPickers } from "../../ui/combobox.js";
import { stationLabel, stationName } from "../stations/index.js";
export const bookmarks = (station) => {
  const target = (id) =>
    station
      ? {
          star: Number(String(id).split("|")[0]),
          object: String(id).split("|")[1],
        }
      : { star: Number(id), object: "" };
  const get = (id) => {
    const t = target(id);
    return Session.data.workbench?.notes?.find(
      (n) => n.star === t.star && n.object_id === t.object,
    );
  };
  return {
    has: (id) => !!get(id)?.bookmark,
    toggle: async (id) => {
      const t = target(id),
        old = get(id);
      await Game.command({
        action: "note",
        star: t.star,
        target: t.object,
        note: old?.note || "",
        bookmark: !old?.bookmark,
      });
    },
  };
};
window.addEventListener("gamedata", () => refreshPickers());
export const stationPicker = (root, ports, value, change) => {
  const picker = UI.picker(
    ports.map((p) => ({
      id: p.star + "|" + (p.station || p.object_id),
      code: String(p.star),
      label: p.name + (p.specialization ? " · " + tr(p.specialization) : ""),
      station: { star: p.star, id: p.station || p.object_id },
      renderLabel: (label) =>
        stationLabel(p.star, p.station || p.object_id, label),
      getLabel: () =>
        stationName(p.star, p.station || p.object_id, p.name) +
        (p.specialization ? " · " + tr(p.specialization) : ""),
    })),
    value,
    change,
    { system: true, station: true, favorites: bookmarks(true) },
  );
  root.append(picker);
  return picker;
};
export const galaxyOf = (id) => world.stars.get(Number(id))?.galaxy || "edem";
export const inPlayerGalaxy = (star) => world.stars.has(Number(star)) && world.stars.has(Number(Session.data.expedition?.ship.star)) && galaxyOf(star) === galaxyOf(Session.data.expedition?.ship.star);
export const systems = (value, change, galaxy = galaxyOf(value)) =>
  UI.picker(
    (world.data?.stars || [])
      .filter((s) => !s.deleted && (s.galaxy || "edem") === galaxy)
      .map((s) => ({
        id: s.id,
        code: String(s.id),
        label: s.name,
        extra:
          (known(s.id) ? "✓ " : "") +
          (Session.data.fleet?.ships.some((ship) => ship.star === s.id)
            ? "◇"
            : ""),
      })),
    value,
    change,
    { system: true, favorites: bookmarks(false) },
  );
