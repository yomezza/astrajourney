import { I18n } from "../../../../web/core/i18n.js";
export const tr = text => I18n.t(text);
export function node(tag, text, cls) {
  const el = document.createElement(tag);
  if (text != null) el.textContent = tr(text);
  if (cls) el.className = cls;
  return el;
}
export function button(text, action) {
  const el = node("button", text);
  el.type = "button";
  el.onclick = action;
  return el;
}
export function field(text, control) {
  const label = node("label", text);
  control.setAttribute("aria-label", tr(text));
  label.append(control);
  return label;
}
export function select(options, value, change) {
  const el = node("select");
  for (const [id, name] of options) {
    const option = node("option", name);
    option.value = id;
    el.append(option);
  }
  el.value = value ?? "";
  el.onchange = () => change(el.value);
  return el;
}
export function table(headers, rows) {
  const el = node("table");
  const head = el.createTHead().insertRow();
  for (const header of headers) {
    const th = node("th", header);
    th.scope = "col";
    head.append(th);
  }
  const body = el.createTBody();
  for (const row of rows) {
    const tr = body.insertRow();
    for (const value of row) tr.insertCell().textContent = value;
  }
  const wrapper = node("div", null, "table-wrap");
  wrapper.append(el);
  return wrapper;
}
export function blocked(labels) {
  const block = node("div", null, "blocked");
  block.append(node("p", "Игровые команды недоступны в прототипе."));
  for (const label of labels) {
    const action = button(label);
    action.disabled = true;
    block.append(action);
  }
  return block;
}
