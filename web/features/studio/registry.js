const module = (label, section, icon, options = {}) =>
  Object.freeze({ label, section, icon, ...options });

// Instance types are stable and checked by internal/workspaces on write.
const follow = {binding:"workspace",legacyWorkspace:true};

export const registry = Object.freeze({
  overview: module("Флот", "Навигация и флот", "ship", { wide:true }),
  fleet: module("Корабль", "Навигация и флот", "ship", { binding:"workspace", expedition:true }),
  cargo: module("Трюм", "Навигация и флот", "cargo", { binding:"workspace" }),
  map: module("Галактическая карта", "Навигация и флот", "galaxy", { ...follow, wide:true, expedition:true }),
  operations: module("Активные операции", "Операции", "tasks", { expedition:true }),
  mining: module("Добыча", "Операции", "research", { binding:"workspace", expedition:true }),
  market: module("Рыночные предложения", "Операции", "market", { ...follow, expedition:true }),
  logistics: module("Логистика", "Операции", "logistics", follow),
  tasks: module("Планировщик задач", "Операции", "tasks", follow),
  studies: module("Исследования", "Развитие", "studies", { ...follow, expedition:true }),
  colonies: module("Колонии", "Развитие", "colonies", { target:"colonies" }),
  corporations: module("Корпорация", "Развитие", "corporations", { target:"corporations" }),
  guide: module("Руководство пилота", "Развитие", "guide", { target:"guide" }),
  finance: module("История финансов", "Связь и настройки", "finance", { target:"finance" }),
  profile: module("Профиль игрока", "Связь и настройки", "profile", { target:"profile" }),
});
