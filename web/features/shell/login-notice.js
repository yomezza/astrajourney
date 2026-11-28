import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { node } from "../../ui/dom.js";

const intro = [
  "Astra Journey — экспериментальный проект, который я создаю при помощи ChatGPT, преимущественно из собственного любопытства и желания посмотреть, насколько далеко можно зайти в создании полноценного игрового мира с использованием искусственного интеллекта.",
  "У проекта нет огромной команды разработчиков. Есть человек, нейросеть, множество идей и постепенно растущая Вселенная.",
  "До альфа-версии мы пока не доросли, поэтому не удивляйтесь, если завтра что-то будет работать совершенно иначе, чем сегодня.",
  "Отдельно надеюсь, что одновременное появление 10–20 пилотов не приведёт к коллапсу всей Вселенной. Если же это произойдёт, будем считать, что мы случайно реализовали механику Большого схлопывания. :)",
  "Если вам интересно не только играть, но и наблюдать за рождением игрового мира или даже участвовать в его создании — добро пожаловать!",
];

export function initLoginNotice() {
  const tr = (text) => I18n.t(text);
  const dialog = node("dialog", undefined, "login-notice");
  dialog.id = "login-notice";
  dialog.setAttribute("aria-labelledby", "login-notice-title");

  const title = node("h2", tr("Немного о том, куда вы попали"));
  title.id = "login-notice-title";
  title.tabIndex = -1;

  const body = node("div", undefined, "login-notice-body");
  body.tabIndex = 0;
  body.setAttribute("aria-labelledby", title.id);
  for (const paragraph of intro) body.append(node("p", tr(paragraph)));
  const ideas = node("p");
  ideas.append(
    tr("В игре существует специальный модуль") + " ",
    node("strong", tr("«Предложить идею»")),
    ", " + tr("через который вы можете поделиться своими мыслями о её дальнейшем развитии."),
  );
  body.append(ideas, node("p", tr("А что из всего этого получится — узнаем вместе.")));

  const actions = node("form", undefined, "login-notice-actions");
  actions.method = "dialog";
  const label = node("label");
  const hide = node("input");
  hide.type = "checkbox";
  label.append(hide, node("span", tr("Больше не показывать")));
  const dismiss = node("button", tr("Продолжить"), "terminal-action");
  dismiss.type = "submit";
  actions.append(label, dismiss);
  dialog.append(title, body, actions);
  document.body.append(dialog);

  let preference;
  hide.onchange = () => {
    if (!preference) return;
    if (hide.checked) Preferences.write(preference, true);
    else Preferences.remove(preference);
  };

  return {
    show(account) {
      preference = Preferences.key("terminal-welcome-hidden", { account });
      if (Preferences.read(preference, false)) return;
      hide.checked = false;
      body.scrollTop = 0;
      dialog.showModal();
      title.focus();
    },
    close() {
      dialog.close();
      preference = undefined;
    },
  };
}
