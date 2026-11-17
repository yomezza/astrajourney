import { Session } from "../core/session.js";
import { Format } from "../core/format.js";
const empty = [];
function index(get, key) {
  let source, values;
  return () => {
    const list = get() || empty;
    if (list !== source) {
      source = list;
      values = new Map(list.map((value) => [key(value), value]));
    }
    return values;
  };
}
const resources = index(
  () => Session.data.expedition?.resources,
  (r) => r.id,
);
const stations = index(
  () => Session.data.workbench?.stations,
  (p) => `${p.star}|${p.object_id}`,
);
const knowledge = index(
  () => Session.data.workbench?.known,
  (p) => p.star,
);
export const d = () => Session.data.expedition,
  w = () => Session.data.workbench || {},
  res = (id) => resources().get(id),
  num = (v) => Format.quantity(v),
  station = () => stations().get(`${d()?.ship.star}|${d()?.ship.object_id}`),
  fmtTime = (value) => Format.timestamp(value),
  money = (v) => Format.credits(v),
  known = (id) => knowledge().has(id),
  fly = (s) => !!s.destination || !!s.local_target,
  cargoCapacity = () => d()?.ship.spec?.cargo || d()?.capacity || 60,
  fuelCapacity = () => d()?.ship.spec?.fuel || 40;
