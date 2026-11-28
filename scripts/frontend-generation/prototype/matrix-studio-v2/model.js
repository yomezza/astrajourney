export const limit = { workspaces: 10, modules: 16 };
const key = "astra-studio-v2-prototype-r1";
export function createModule(type, purpose, binding) {
  return { id: crypto.randomUUID(), type, purpose, binding: structuredClone(binding), settings: {}, wide: false };
}
export function starter() {
  const id = crypto.randomUUID();
  return { active: id, start: id, workspaces: [{
    id, name: "Командование", ship: "watch", modules: [
      createModule("ship", "owned", { mode: "workspace" }),
      createModule("navigation", "interstellar", { mode: "workspace" }),
      createModule("inventory", "cargo", { mode: "workspace" }),
    ],
  }] };
}
export function restore(storage = localStorage) {
  try {
    const layout = JSON.parse(storage.getItem(key));
    if (layout?.workspaces?.some(space => space.id === layout.active)) return layout;
  } catch {
    // A denied or damaged prototype store falls back to the starter.
  }
  return starter();
}
export function save(layout, storage = localStorage) {
  try {
    storage.setItem(key, JSON.stringify(layout));
    return true;
  } catch {
    return false;
  }
}
export function anchor(binding, workspace) {
  if (binding.mode === "workspace") return { mode: "fixed", id: workspace.ship };
  if (binding.mode === "derived") return { ...binding, ship: workspace.ship };
  return { ...binding };
}
export function resolve(binding, workspace, data) {
  if (binding.mode === "station") {
    const station = data.stations[binding.id];
    return station ? { station } : { missing: "Объект недоступен" };
  }
  const id = binding.mode === "workspace" ? workspace.ship :
    binding.mode === "derived" ? Object.hasOwn(binding, "ship") ? binding.ship : workspace.ship : binding.id;
  if (!id) return { missing: "Выберите корабль пространства" };
  const ship = data.ships[id];
  if (!ship) return { missing: "Объект недоступен" };
  if (binding.mode !== "derived") return { ship };
  return data.stations[ship.dock] ? { ship, station: data.stations[ship.dock] } :
    { ship, missing: "Корабль не пристыкован к станции" };
}
