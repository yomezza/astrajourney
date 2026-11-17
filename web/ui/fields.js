import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";
import { Format } from "../core/format.js";

const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function initFields() {
  UI.field = (root, label, id, value, type = "text", help = "") => {
    const l = n("label"),
      caption = n("span", tr(label)),
      i = n("input");
    l.append(caption, i);
    i.id = id;
    i.type = type;
    i.value = value ?? "";
    if (help) l.dataset.help = help;
    if (type === "number") {
      i.min = 0;
      i.step = 1;
    }
    root.append(l);
    return i;
  };
  UI.row = (root, label, value) => {
    const r = n("div", undefined, "row");
    r.append(
      n("span", tr(label)),
      value instanceof Node ? value : n("b", String(value ?? "—")),
    );
    root.append(r);
    return r;
  };
  UI.bar = (value, max, label) => {
    const wrap = n("div", undefined, "meter"),
      bar = n("progress");
    bar.max = max;
    bar.value = value;
    bar.setAttribute("aria-label", tr(label));
    wrap.append(
      n("span", tr(label)),
      n("b", `${Format.number(value)} / ${Format.number(max)}`),
      bar,
    );
    return wrap;
  };
  UI.empty = (root, text) => root.append(n("p", tr(text), "empty-state"));
  UI.dirty = (form, save, cancel) => {
    const bar = n("div", undefined, "save-bar");
    bar.hidden = true;
    const status = n("span", tr("Есть несохранённые изменения")),
      yes = UI.button("Сохранить", async () => {
        if (!form.reportValidity()) return false;
        yes.disabled = true;
        status.textContent = tr("Сохранение…");
        try {
          const ok = await save();
          if (ok !== false) {
            bar.hidden = true;
            form.dataset.dirty = "false";
            form
              .querySelectorAll(".field-dirty")
              .forEach((e) => e.classList.remove("field-dirty"));
          }
        } finally {
          yes.disabled = false;
          status.textContent = tr("Есть несохранённые изменения");
        }
      }),
      no = UI.button("Отменить", async () => {
        await cancel();
        bar.hidden = true;
        form.dataset.dirty = "false";
        form
          .querySelectorAll(".field-dirty")
          .forEach((e) => e.classList.remove("field-dirty"));
      });
    bar.append(status, yes, no);
    form.append(bar);
    const dirty = () => {
      form.dataset.dirty = "true";
      bar.hidden = false;
    };
    form.addEventListener("input", dirty);
    form.addEventListener("change", dirty);
    form.onsubmit = (e) => {
      e.preventDefault();
      if (!bar.hidden) yes.click();
    };
    return dirty;
  };
}
