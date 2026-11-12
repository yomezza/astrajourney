import { I18n } from "../../core/i18n.js";

const categories = {
  research_complete: "research",
  delivery_complete: "economy",
  company_invitation: "corporation",
  project_complete: "corporation",
  colony: "corporation",
  arrival: "gameplay",
  landing_ready: "gameplay",
  mining_complete: "gameplay",
  portal_open: "gameplay",
};
export const categoryNames = {
  system: "Системное",
  economy: "Экономика",
  research: "Исследования",
  corporation: "Корпорация",
  gameplay: "Игровое",
};
export const notificationCategory = (item) =>
  Object.hasOwn(categories, item.kind) ? categories[item.kind] : "system";
// The approved A prototype's local monochrome paths, without its mock model.
const glyphs = {
  system:
    "M12 3v2m0 14v2M3 12h2m14 0h2M5.6 5.6l1.4 1.4m10 10 1.4 1.4M18.4 5.6 17 7m-10 10-1.4 1.4M12 8a4 4 0 1 0 0 8a4 4 0 0 0 0-8Z",
  economy: "M3 7h18v11H3zM3 10h18M7 15h4M7 4h10",
  research:
    "M9 3h6M12 3v6m-5 12h10M7.5 21l2-6 5-2 2-4M12 8a3 3 0 1 0 0 6a3 3 0 0 0 0-6Z",
  corporation: "M3 20h18M5 20V9l7-4 7 4v11M9 13h1m4 0h1m-6 4h1m4 0h1M12 5V3",
  gameplay: "M12 3l7 4v5c0 4-3 7-7 9-4-2-7-5-7-9V7zM9 12l2 2 4-4",
};
export function notificationIcon(category) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  for (const [key, value] of Object.entries({
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    "stroke-width": "1.65",
    "stroke-linecap": "round",
    "stroke-linejoin": "round",
    "aria-hidden": "true",
  }))
    svg.setAttribute(key, value);
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", glyphs[category] || glyphs.system);
  svg.append(path);
  return svg;
}
export function notificationTime(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "—";
  const minutes = Math.max(
    0,
    Math.floor((Date.now() - date.getTime()) / 60000),
  );
  if (minutes < 1) return I18n.t("только что");
  if (minutes < 60) return I18n.t("{count} мин", { count: minutes });
  if (minutes < 1440)
    return I18n.t("{count} ч", { count: Math.floor(minutes / 60) });
  return new Intl.DateTimeFormat(I18n.language, {
    day: "numeric",
    month: "short",
  }).format(date);
}
