import { Modules, UI } from "../../ui/runtime.js";
import { node as n } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
const tr = (s) => I18n.t(s);
export function moderationPanel(client, onError) {
  const root = Modules.create("chat-moderation", "Модерация чата", 760, 650, {
    private: true,
  });
  root.classList.add("chat-moderation-body");
  let selected = "",
    generation = 0;
  const b = (label, action) => {
    const el = n("button", tr(label));
    el.onclick = () => Promise.resolve(action()).catch(onError);
    return el;
  };
  async function load(view = "reports") {
    const token = ++generation;
    const rows = await client.get(view);
    if (token !== generation) return;
    root.replaceChildren();
    const tabs = n("nav", undefined, "chat-moderation-tabs");
    tabs.append(
      b("Жалобы", () => load()),
      b("Журнал модерации", () => load("audit")),
      b("Назначение модератора", grant),
      b("Служебный доступ", service),
    );
    root.append(tabs);
    for (const row of rows) {
      const item = n("article", undefined, "chat-moderation-row");
      if (view === "audit") {
        item.append(
          n("span", new Date(row.created_at).toLocaleString(I18n.language)),
          n("b", row.action),
          n("p", row.reason || ""),
        );
      } else {
        item.append(
          n("b", row.reporter + " · " + row.kind),
          n("p", row.reason),
          n("span", row.state),
        );
        if (["open", "in_review"].includes(row.state))
          item.append(b("Рассмотреть", () => inspect(row.id)));
      }
      root.append(item);
    }
    if (!rows.length) root.append(n("p", tr("Нет записей"), "chat-empty"));
  }
  async function inspect(id) {
    selected = id;
    const rows = await client.get("report", { id });
    if (selected !== id) return;
    const report = rows[0];
    root.replaceChildren(b("← К жалобам", () => load()));
    if (!report) return;
    root.append(
      n("p", report.reason),
      n("b", report.author || tr("Пилот")),
      n("p", report.body || tr("Сообщение удалено или срок хранения истёк")),
    );
    const reason = n("input");
    reason.placeholder = tr("Причина удаления");
    reason.maxLength = 200;
    root.append(reason);
    if (report.body)
      root.append(
        b("Удалить сообщение", async () => {
          await client.command("message.delete", {
            id: report.message_id,
            reason: reason.value,
          });
          await inspect(id);
        }),
      );
    for (const [decision, label] of [
      ["resolved", "Закрыть жалобу"],
      ["rejected", "Отклонить жалобу"],
    ])
      root.append(
        b(label, async () => {
          await client.command("report.resolve", { id, decision });
          await load();
        }),
      );
  }
  async function service() {
    const channels = await client.get("service.channels");
    root.replaceChildren(b("← К жалобам", () => load()));
    for (const channel of channels)
      root.append(
        b(
          channel.kind +
            " · " +
            (channel.company || channel.planet || "Global") +
            (channel.sector_number ? " / " + channel.sector_number : ""),
          async () => {
            const messages = await client.get("service.history", {
              id: channel.id,
            });
            root.replaceChildren(b("← Каналы", service));
            for (const m of messages) {
              const row = n("article", undefined, "chat-moderation-row");
              row.append(n("b", m.author), n("p", m.body));
              root.append(row);
            }
          },
        ),
      );
  }
  function grant() {
    root.replaceChildren(b("← К жалобам", () => load()));
    const user = n("input");
    user.placeholder = tr("ID пилота");
    user.setAttribute("aria-label", user.placeholder);
    root.append(
      user,
      b("Назначить модератора", () =>
        api("admin/chat/moderator", { user: user.value, granted: true }),
      ),
      b("Отозвать полномочия", () =>
        api("admin/chat/moderator", { user: user.value, granted: false }),
      ),
    );
  }
  Modules.open("chat-moderation");
  load().catch(onError);
}

export function corporateModeration(client, onError) {
  const root = Modules.create(
    "chat-corporate-moderation",
    "Модерация корпорации",
    650,
    600,
    { private: true },
  );
  root.classList.add("chat-moderation-body");
  async function load() {
    const data = await client.get("corporate.moderation");
    root.replaceChildren(n("h3", tr("Действующие ограничения")));
    for (const mute of data.mutes) {
      const row = n("article", undefined, "chat-moderation-row"),
        cancel = n("button", tr("Снять ограничение"));
      cancel.onclick = async () => {
        try {
          await client.command("unmute", {
            target: data.company,
            user: mute.user_id,
            reason: tr("Ограничение снято модератором"),
          });
          await load();
        } catch (e) {
          onError(e);
        }
      };
      row.append(
        n("b", mute.username),
        n("p", mute.reason),
        n("time", new Date(mute.expires_at).toLocaleString(I18n.language)),
        cancel,
      );
      root.append(row);
    }
    root.append(n("h3", tr("История модерации")));
    for (const event of data.events) {
      const row = n("article", undefined, "chat-moderation-row");
      row.append(
        n("span", event.action),
        n("p", event.reason),
        n("time", new Date(event.created_at).toLocaleString(I18n.language)),
      );
      root.append(row);
    }
  }
  Modules.open("chat-corporate-moderation");
  load().catch(onError);
}
