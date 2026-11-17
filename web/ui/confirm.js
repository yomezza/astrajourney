import { UI, Modules } from "./runtime.js";
import { node } from "./dom.js";
import { Session } from "../core/session.js";
let confirmGeneration = 0;
export function initConfirm() {
  UI.confirm = function confirm(title, text, run) {
    const generation = ++confirmGeneration;
    const epoch = Session.epoch;
    const root = Modules.create("confirm", title, 470, 340, {
      transient: true,
    });
    root.replaceChildren(node("p", text));
    const yes = UI.button("Подтвердить", async () => {
        const accepted = await run();
        if (
          accepted &&
          generation === confirmGeneration &&
          epoch === Session.epoch &&
          root.isConnected
        )
          Modules.close("confirm");
      }),
      no = UI.button("Отмена", () => Modules.close("confirm"));
    root.append(yes, no);
    Modules.open("confirm");
  };
}
