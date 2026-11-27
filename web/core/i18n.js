import { Preferences } from "./preferences.js";

const supportedLanguages = ["ru"];
const preferred = Preferences.language();
const initial = supportedLanguages.includes(preferred) ? preferred : supportedLanguages[0];
if (initial !== preferred) Preferences.setLanguage(initial);

export const I18n = {
  language: initial,
  catalogs: {},
  t(text, parameters = {}) {
    const catalog = this.catalogs[this.language] || {};
    const translated =
      catalog[text] ??
      (typeof text === "string" && text.includes(" · ")
        ? text
            .split(" · ")
            .map((part) => catalog[part] ?? part)
            .join(" · ")
        : text);
    return typeof translated === "string"
      ? translated.replace(/\{(\w+)\}/g, (match, key) =>
          Object.hasOwn(parameters, key) ? String(parameters[key]) : match,
        )
      : translated;
  },
  apply() {
    document.documentElement.lang = this.language;
    document.querySelectorAll("[data-i18n]").forEach((el) => {
      el.textContent = this.t(el.dataset.i18n);
    });
    document.querySelectorAll("[data-placeholder]").forEach((el) => {
      el.placeholder = this.t(el.dataset.placeholder);
    });
    document
      .querySelectorAll("[data-label]")
      .forEach((el) => el.setAttribute("aria-label", this.t(el.dataset.label)));
    document.querySelectorAll("[data-nav]").forEach((el) => {
      el.title = this.t(el.dataset.nav);
      el.setAttribute("aria-label", this.t(el.dataset.nav));
    });
  },
  set(language) {
    if (!supportedLanguages.includes(language)) return;
    this.language = language;
    Preferences.setLanguage(language);
    this.apply();
    window.dispatchEvent(new Event("languagechange"));
  },
};
export function initI18n() {
  I18n.ready = Promise.all(
    supportedLanguages.map(async (language) => {
      const r = await fetch(new URL("../" + language + ".json", import.meta.url));
      if (!r.ok) throw Error("Не удалось загрузить словарь интерфейса");
      I18n.catalogs[language] = await r.json();
    }),
  )
    .then(() => I18n.apply())
    .catch((e) => {
      console.error(e);
    });

  return I18n.ready;
}
