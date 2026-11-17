import { I18n } from "./i18n.js";
export const Format = {
  number(value, options = {}) {
    return typeof value === "number" && Number.isFinite(value)
      ? value.toLocaleString(I18n.language, {
          maximumFractionDigits: 1,
          ...options,
        })
      : "—";
  },
  quantity: (value) =>
    Format.number(Number(value ?? 0), { maximumFractionDigits: 3 }),
  credits: (value) =>
    Format.number(Number(value ?? 0), { maximumFractionDigits: 0 }) + " CR",
  percent: (value) =>
    typeof value === "number" && Number.isFinite(value)
      ? Format.number(Math.max(0, Math.min(100, value))) + "%"
      : "—",
  science: (value) => Format.number(value, { maximumSignificantDigits: 4 }),
  timestamp: (value, options) =>
    value && Number.isFinite(new Date(value).getTime())
      ? new Date(value).toLocaleString(I18n.language, options)
      : "—",
  time: (value) => new Date(value).toLocaleTimeString(I18n.language),
  area: (value) => Format.number(value, { maximumFractionDigits: 0 }) + " km²",
};
