import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";

import { placeFloating, trackFloating, overlayHost } from "./floating.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export let refreshPickers = () => {};
export function initCombobox() {
  let current,
    pickerSerial = 0;
  const pickerRegistry = new WeakMap();
  const refreshLabels = () => {
    for (const button of document.querySelectorAll(".picker-button")) {
      const entry = pickerRegistry.get(button);
      if (!entry) continue;
      for (const option of entry.opts)
        option.label = option.raw ? option.source : I18n.t(option.source);
      entry.update();
    }
  };
  window.addEventListener("languagechange", refreshLabels);
  refreshPickers = refreshLabels;
  UI.picker = (options, value, change, config = {}) => {
    const normalize = (options) =>
      options.map((o) =>
        Array.isArray(o)
          ? {
              id: String(o[0]),
              label: tr(o[1]),
              source: o[1],
              raw: !!config.raw,
            }
          : { ...o, id: String(o.id), source: o.label, raw: true },
      );
    const opts = normalize(options),
      button = UI.button("", () => {});
    button.classList.add("picker-button");
    if (config.system) button.classList.add("system-picker");
    button.setAttribute("role", "combobox");
    button.setAttribute("aria-expanded", "false");
    button.setAttribute("aria-haspopup", "listbox");
    button.value = String(value ?? opts[0]?.id ?? "");
    const update = () => {
      for (const option of opts)
        if (option.getLabel) option.label = option.getLabel();
      const selected = opts.find((o) => o.id === button.value);
      button.replaceChildren();
      button.classList.toggle("picker-has-code", !!selected?.code);
      if (selected?.code) button.append(n("span", selected.code, "system-id"));
      button.append(
        selected?.renderLabel
          ? selected.renderLabel(selected.label)
          : n(
              "span",
              selected?.label || tr("Выберите значение"),
              "picker-label",
            ),
        n("span", "⌄", "picker-chevron"),
      );
    };
    update();
    pickerRegistry.set(button, { opts, update });
    button.setOptions = (options) => {
      current?.button === button && current.close();
      opts.splice(0, opts.length, ...normalize(options));
      if (!opts.some((option) => option.id === button.value))
        button.value = opts[0]?.id || "";
      update();
    };
    button.setValue = (value) => {
      const next = String(value);
      if (!opts.some((option) => option.id === next)) return;
      button.value = next;
      update();
    };
    button.onclick = () => {
      if (current?.button === button) {
        current.close();
        return;
      }
      current?.close();
      const pop = n("div", undefined, "picker-popover"),
        search = n("input"),
        list = n("div", undefined, "picker-list"),
        footer = n("div", undefined, "pagination");
      if (config.system) pop.classList.add("system-popover");
      let page = 1,
        focusIndex = -1;
      search.type = "search";
      search.placeholder = tr(
        config.system ? "Название или ID системы" : "Поиск",
      );
      search.setAttribute("aria-label", search.placeholder);
      const favorites = n("div", undefined, "picker-favorites"),
        columns = n("div", undefined, "picker-columns");
      if (config.favorites) {
        const all = n("div");
        all.append(
          n(
            "div",
            tr(config.station ? "Все станции" : "Все системы"),
            "picker-column-title",
          ),
          list,
        );
        columns.append(all, favorites);
        pop.append(search, columns, footer);
      } else pop.append(search, list, footer);
      overlayHost(button).append(pop);
      let tracking;
      button.setAttribute("aria-expanded", "true");
      const close = () => {
        tracking?.dispose();
        pop.remove();
        button.removeAttribute("aria-controls");
        button.setAttribute("aria-expanded", "false");
        document.removeEventListener("pointerdown", outside);
        current = null;
      };
      current = { close, button };
      const outside = (e) => {
        if (!pop.contains(e.target) && !button.contains(e.target)) close();
      };
      document.addEventListener("pointerdown", outside);
      const render = () => {
        const q = search.value.toLowerCase().trim(),
          filtered = opts.filter((o) =>
            (o.label + " " + (o.code || "") + " " + o.id)
              .toLowerCase()
              .includes(q),
          ),
          size = config.pageSize || 10,
          pages = Math.max(1, Math.ceil(filtered.length / size));
        page = Math.min(page, pages);
        list.replaceChildren();
        list.setAttribute("role", "listbox");
        footer.replaceChildren();
        const optionRow = (o, host) => {
          const b = UI.button("", async () => {
            button.value = o.id;
            update();
            close();
            button.focus();
            await change?.(button.value);
            await button.onchange?.({ target: button });
          });
          b.classList.add("picker-option");
          b.actionOwner = button;
          b.classList.toggle("picker-has-code", !!o.code);
          b.setAttribute("role", "option");
          b.setAttribute("aria-selected", String(o.id === button.value));
          if (o.code) b.append(n("span", o.code, "system-id"));
          b.append(
            o.renderLabel
              ? o.renderLabel(o.label)
              : n("span", o.label, "picker-label"),
          );
          if (o.extra) b.append(n("small", o.extra));
          if (config.favorites) {
            const row = n("div", undefined, "picker-entry"),
              fav = UI.button(
                config.favorites.has(o.id) ? "★" : "☆",
                async () => {
                  await config.favorites.toggle(o.id);
                  render();
                },
              );
            fav.className = "favorite-toggle";
            fav.dataset.help = "Избранное";
            fav.setAttribute("aria-label", tr("Избранное") + ": " + o.label);
            row.append(b, fav);
            host.append(row);
          } else host.append(b);
        };
        for (const o of filtered.slice((page - 1) * size, page * size))
          optionRow(o, list);
        if (config.favorites) {
          favorites.replaceChildren(
            n("div", tr("Избранное"), "picker-column-title"),
          );
          const saved = filtered.filter((o) => config.favorites.has(o.id));
          for (const o of saved) optionRow(o, favorites);
          if (!saved.length) UI.empty(favorites, "Нет избранных объектов");
        }
        if (!filtered.length) UI.empty(list, "Ничего не найдено");
        footer.append(
          UI.button(
            "‹",
            () => {
              page--;
              render();
            },
            page === 1,
          ),
          n(
            "span",
            `${filtered.length ? (page - 1) * size + 1 : 0}–${Math.min(page * size, filtered.length)} / ${filtered.length}`,
          ),
          UI.button(
            "›",
            () => {
              page++;
              render();
            },
            page === pages,
          ),
        );
        position();
      };
      const position = () =>
        placeFloating(pop, {
          anchor: button,
          minTop: 65,
          maxBottom: 40,
          minWidth: config.favorites ? 620 : config.system ? 350 : 260,
        });
      tracking = trackFloating({
        element: pop,
        owner: button,
        position,
        close,
      });
      search.oninput = () => {
        page = 1;
        focusIndex = -1;
        render();
      };
      pop.onkeydown = (e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          close();
          button.focus();
        }
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          e.preventDefault();
          const buttons = [...list.querySelectorAll("button")];
          focusIndex = Math.max(
            0,
            Math.min(
              buttons.length - 1,
              focusIndex + (e.key === "ArrowDown" ? 1 : -1),
            ),
          );
          buttons[focusIndex]?.focus();
        }
        if (e.key === "Enter" && e.target === search) {
          e.preventDefault();
          list.querySelector("button")?.click();
        }
      };
      render();
      list.id = "picker-list-" + ++pickerSerial;
      button.setAttribute("aria-controls", list.id);
      search.setAttribute("aria-controls", list.id);
      search.focus();
    };
    return button;
  };
  UI.choice = (root, label, options, value, change) => {
    const l = n("label");
    l.append(n("span", tr(label)));
    const picker = UI.picker(options, value, change);
    picker.dataset.field = label;
    l.append(picker);
    root.append(l);
    return picker;
  };
}
