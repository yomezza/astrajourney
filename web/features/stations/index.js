import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";
import { Game } from "../../features/runtime.js";
import { world } from "../../core/world-store.js";

const tr = (s) => I18n.t(s),
  n = (...args) => UI.node(...args);
const own = (star, station) =>
  Session.data.workbench?.notes?.find(
    (note) => note.star === Number(star) && note.object_id === station,
  );
function sites(data, lookup = own) {
  const star = data.star?.id || data.system?.id;
  for (const site of data.sites || [])
    if (site.kind === "Орбитальная станция") {
      const note = lookup(star, site.id);
      site.canonical_name ??= site.name;
      site.name = note?.label || site.canonical_name;
      site.personal_note = note?.note || "";
    }
}
export const personalizeStations = (data) => {
  if (!data || typeof data !== "object") return data;
  // Presentation projection: never rewrite the transport/raw snapshot DTO.
  data = { ...data };
  const notes = data.workbench?.notes || Session.data.workbench?.notes || [];
  const lookup = (star, id) =>
    notes.find((note) => note.star === Number(star) && note.object_id === id);
  for (const key of ["ports", "stations", "sites"])
    if (Array.isArray(data[key]))
      data[key] = data[key].map((item) => ({ ...item }));
  if (data.expedition)
    data.expedition = {
      ...data.expedition,
      sites: (data.expedition.sites || []).map((item) => ({ ...item })),
    };
  if (data.workbench)
    data.workbench = {
      ...data.workbench,
      stations: (data.workbench.stations || []).map((item) => ({ ...item })),
      shipyards: (data.workbench.shipyards || []).map((item) => ({ ...item })),
    };
  for (const list of [data.ports, data.stations])
    if (Array.isArray(list))
      for (const port of list) {
        const station = port.object_id || port.station;
        if (!station || !port.name) continue;
        port.canonical_name ??= port.name;
        port.name = lookup(port.star, station)?.label || port.canonical_name;
      }
  if (data.workbench) {
    for (const port of data.workbench.stations || []) {
      const note = lookup(port.star, port.object_id);
      port.canonical_name ??= port.name;
      port.name = note?.label || port.canonical_name;
      port.personal_note = note?.note || "";
    }
    for (const yard of data.workbench.shipyards || []) {
      yard.canonical_name ??= yard.station_name;
      yard.station_name =
        lookup(yard.star, yard.station)?.label || yard.canonical_name;
    }
    if (data.expedition) sites(data.expedition, lookup);
  } else sites(data, lookup);
  return data;
};
function updateLabel(label) {
  const star = Number(label.dataset.stationStar),
    station = label.dataset.stationObject,
    note = own(star, station),
    port = Session.data.workbench?.stations?.find(
      (p) => p.star === star && p.object_id === station,
    ),
    subject = note?.label || port?.name || label.dataset.stationFallback;
  const version = JSON.stringify([subject, note?.note || ""]);
  if (label.dataset.stationVersion === version) return;
  label.dataset.stationVersion = version;
  const text = n("span", subject);
  if (note?.note) {
    text.className = "station-personal-note";
    text.dataset.helpRaw = note.note;
    text.dataset.help = "Личная заметка";
    text.tabIndex = 0;
  }
  label.replaceChildren(
    document.createTextNode(label.dataset.stationPrefix),
    text,
    document.createTextNode(label.dataset.stationSuffix),
  );
}
export const stationLabel = (star, station, name, tag = "span") => {
  const note = own(star, station),
    port = Session.data.workbench?.stations?.find(
      (p) => p.star === Number(star) && p.object_id === station,
    ),
    subject = note?.label || port?.name || name,
    label = n(tag, undefined, "station-label picker-label"),
    value = String(name);
  let match = String(subject),
    at = value.indexOf(match);
  if (at < 0 && port?.canonical_name) {
    match = port.canonical_name;
    at = value.indexOf(match);
  }
  Object.assign(label.dataset, {
    stationStar: String(star),
    stationObject: station,
    stationFallback: String(subject),
    stationPrefix: at >= 0 ? value.slice(0, at) : "",
    stationSuffix: at >= 0 ? value.slice(at + match.length) : "",
  });
  updateLabel(label);
  label.oncontextmenu = (event) => {
    event.preventDefault();
    event.stopPropagation();
    UI.menu(
      [
        {
          label: "Личное название и заметка",
          action: () => stationEditor(star, station),
        },
      ],
      event.clientX,
      event.clientY,
    );
  };
  return label;
};
export const stationRef = (star, station) => {
  const port = Session.data.workbench?.stations?.find(
    (p) => p.star === Number(star) && p.object_id === station,
  );
  return stationLabel(
    star,
    station,
    port?.name ||
      (world.data?.stars.find((s) => s.id === Number(star))?.name || star) +
        " · " +
        station,
    "b",
  );
};
export const stationEditor = (star, station) => {
  const port = Session.data.workbench?.stations?.find(
    (p) => p.star === Number(star) && p.object_id === station,
  );
  if (!port) return;
  const note = own(star, station),
    root = Modules.create("station-label", "Личное название станции", 540, 510);
  root.replaceChildren(
    n("h3", port.canonical_name || port.name),
    n(
      "p",
      tr(
        "Название и заметка видны только вам. Пустое название возвращает исходное имя станции.",
      ),
      "muted",
    ),
  );
  const name = UI.field(
    root,
    "Личное название",
    "station-personal-name",
    note?.label || "",
  );
  name.maxLength = 120;
  const label = n("label"),
    text = n("textarea");
  text.id = "station-personal-note";
  text.rows = 7;
  text.value = note?.note || "";
  label.append(n("span", tr("Личная заметка")), text);
  root.append(label);
  root.append(
    UI.button("Сохранить", async () => {
      if (
        Array.from(name.value.trim()).length > 60 ||
        Array.from(text.value).length > 800
      ) {
        notify(tr("Личное название: до 60 символов, заметка: до 800"), "error");
        return;
      }
      if (
        await Game.command({
          action: "station_label",
          star: Number(star),
          target: station,
          name: name.value,
          note: text.value,
        })
      )
        Modules.close("station-label");
    }),
    UI.button("Исходное название", () => {
      name.value = "";
      name.focus();
    }),
  );
  Modules.open("station-label");
};
export const stationName = (star, id, fallback) =>
  own(star, id)?.label ||
  Session.data.workbench?.stations?.find(
    (p) => p.star === Number(star) && p.object_id === id,
  )?.name ||
  fallback;
export function initStationLabels() {
  window.addEventListener("gamedata", () =>
    document.querySelectorAll("[data-station-star]").forEach(updateLabel),
  );
  window.addEventListener("signedout", () =>
    document
      .querySelector("#window-station-label .module-content")
      ?.replaceChildren(),
  );
}
