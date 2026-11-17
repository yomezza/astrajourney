import { Session, RequestScope } from "../../../core/session.js";
import { api } from "../../../core/api.js";
import { I18n } from "../../../core/i18n.js";
import { UI, Modules } from "../../../ui/runtime.js";

const labels = {
  ready: "Готова",
  disabled: "Отключена",
  unreachable: "Нет связи",
  not_ready: "Не готова",
  configuration_error: "Ошибка настройки",
  mismatch: "Другая игровая БД",
};
export function createAdministrationServices({ root }) {
  const tr = (s) => I18n.t(s),
    n = (...a) => UI.node(...a);
  const scope = new RequestScope(
    () =>
      Session.signedIn &&
      Session.data.state?.user.role === "admin" &&
      Modules.isOpen("services"),
  );
  let timer;
  const close = () => {
    clearTimeout(timer);
    scope.invalidate();
  };
  async function render() {
    clearTimeout(timer);
    const task = scope.begin();
    if (!task.current()) return false;
    if (!root.firstChild)
      root.append(n("p", tr("Проверяем подключение служб…"), "muted"));
    try {
      const data = await api(
        "admin/services",
        undefined,
        AbortSignal.any([task.signal, AbortSignal.timeout(5000)]),
      );
      if (!task.current()) return false;
      const cards = n("div", undefined, "service-cards");
      for (const service of data.services) {
        const card = n("section", undefined, "service-card");
        card.dataset.service = service.id;
        const heading = n("div", undefined, "service-card-heading");
        const badge = n(
          "span",
          tr(labels[service.status] || "Нет данных"),
          "service-status-tag",
        );
        badge.dataset.status = service.status;
        badge.setAttribute("role", "status");
        heading.append(n("h3", tr(service.name)), badge);
        const natsLabels={connected:"Подключён",disconnected:"Нет соединения",connecting:"Подключение…",disabled:"Отключён в настройках",not_used:"Не используется режимом чата",unknown:"Нет данных"};
        const state=service.nats?.state || "unknown";
        const transport=n("span","NATS · "+tr(natsLabels[state] || "Нет данных"),"service-status-tag service-nats-tag");
        transport.dataset.status=state;transport.setAttribute("role","status");
        heading.append(transport);
        const info = n("dl", undefined, "service-info");
        for (const field of service.info)
          info.append(n("dt", tr(field.label)), n("dd", tr(field.value)));
        card.append(heading, n("hr"));
        if(state==="not_used")card.append(n("p",tr("Для NATS выберите dispatcher_mode = b1 в настройках Chat"),"service-reason"));
        if(service.nats?.error_code)card.append(n("p",service.nats.error_code,"service-reason"));

        if (service.reason)
          card.append(n("p", tr(service.reason), "service-reason"));
        card.append(info);
        cards.append(card);
      }
      const checked = n("p", undefined, "muted service-checked");
      checked.textContent =
        tr("Проверено") +
        ": " +
        new Intl.DateTimeFormat(I18n.language || "ru", {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        }).format(new Date(data.checked_at)) +
        " · " +
        tr("Обновление каждые 5 секунд");
      root.replaceChildren(cards, checked);
    } catch (e) {
      if (task.current())
        root.replaceChildren(
          n(
            "p",
            tr("Не удалось обновить статусы служб. Повторяем проверку…"),
            "service-reason",
          ),
        );
    } finally {
      if (task.current()) timer = setTimeout(() => render(), 5000);
    }
  }
  return { render, close, clear: close };
}
