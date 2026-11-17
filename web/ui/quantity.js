import { UI, Modules } from "./runtime.js";
import { I18n } from "../core/i18n.js";
import { Format } from "../core/format.js";
import { reset } from "./view-state.js";
const n = (...args) => UI.node(...args),
  field = (...args) => UI.field(...args),
  b = (...args) => UI.button(...args),
  tr = (s) => I18n.t(s),
  num = Format.quantity;
export function actionAmount(title, label, max, run, initial = 1) {
  const root = Modules.create("amount", title, 420, 330, { transient: true });
  reset(root);
  const q = field(root, label, "action-quantity", initial, "number");
  q.min = 1;
  q.max = max;
  const result = n("p", undefined, "muted");
  root.append(result);
  const submit = b("Подтвердить", async () => {
    if (await run(Number(q.value))) Modules.close("amount");
  });
  const update = () => {
    submit.disabled =
      !Number.isInteger(Number(q.value)) ||
      Number(q.value) < 1 ||
      Number(q.value) > max;
    result.textContent = tr("Доступно") + ": " + num(max);
  };
  q.oninput = update;
  root.append(submit);
  update();
  Modules.open("amount");
}
