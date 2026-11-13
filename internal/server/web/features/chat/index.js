import { uuid } from "../../core/uuid.js";
import { Session } from "../../core/session.js";
import { Preferences } from "../../core/preferences.js";
import { I18n } from "../../core/i18n.js";
import { Modules, UI } from "../../ui/runtime.js";
import { node as n } from "../../ui/dom.js";
import { openPlanet } from "../planet/loader.js";
import { Game } from "../runtime.js";
import { initChatTooltips } from "./tooltips.js";
import { ChatClient } from "./transport.js";
import { moderationPanel, corporateModeration } from "./moderation.js";
import { renderMessages, emojiPicker } from "./messages.js";

const tabs = [
  ["favorites", "Избранное"],
  ["global", "Глобальный"],
  ["planet", "Планета"],
  ["sector", "Сектор"],
  ["corporation", "Корпорация"],
  ["private", "Личные"],
];
const tr = (s) => I18n.t(s);
const button = (text, action, cls = "chat-small-btn") => {
  const b = n("button", tr(text), cls);
  b.type = "button";
  if (typeof text === "string") b.dataset.i18n = text;
  b.onclick = (event) =>
    Promise.resolve(action?.(event)).catch((error) => showError(error));
  return b;
};
let errorSink;
function showError(error) {
  errorSink?.(error);
}
const errors = {
  request_failed: "Не удалось выполнить действие",
  server_busy: "Сервер занят. Попробуйте немного позже.",
  message_missing: "Сообщение недоступно",
  stale_connection: "Переподключитесь к чату",
  conversation_deleting: "Беседа удаляется",
  owner_required: "Выберите нового владельца перед выходом",
  confirmation_required: "Подтвердите удаление",
  reason_must_not_quote_message:
    "Укажите причину без цитирования исходного сообщения",

  chat_unavailable: "Служба чата недоступна",
  cosmic_unavailable: "Космическая служба недоступна",
  planet_unavailable: "Планетарная служба недоступна",
  chat_connection_rejected:
    "Подключение отклонено. Возможно, чат открыт в другой вкладке.",
  rate_limited: "Слишком часто. Подождите перед повторной отправкой.",
  muted: "Вы временно не можете писать в чат корпорации",
  access_denied: "Доступ закрыт",
  auth_required: "Войдите в игру",
  operation_unknown:
    "Результат операции уточняется. Не отправляйте сообщение повторно.",
  operation_not_committed: "Операция не выполнена. Можно повторить отправку.",
  invitation_cooldown:
    "После отказа повторное приглашение доступно через 24 часа",
  invitation_pending: "Приглашение уже отправлено",
  invalid_recipient: "Пилот не найден",
  group_capacity: "Достигнут лимит участников",
  conversation_pending: "Ожидается принятие приглашения",
  search_too_short: "Введите не менее трёх символов",
  presence_unverifiable: "Нет подтверждённого присутствия на планете",
  scope_changed: "Местоположение изменилось",
  revision_conflict: "Данные беседы изменились. Повторите выбор.",
  already_member: "Пилот уже участвует в беседе",
  reason_required: "Укажите причину",
  invalid_argument: "Проверьте введённые данные",
};
export function initChat() {
  const content = Modules.create("chat", "Чат", 420, 540, { private: true }),
    root = n("section", undefined, "chat-root");
  root.setAttribute("aria-label", tr("Чат"));
  content.append(root);
  const dialog = content.closest("dialog");
  dialog.classList.add("chat-window");
  const slot = n("div");
  slot.id = "chat-home-slot";
  const grid = n("div", undefined, "home-lower-grid");
  grid.hidden = true;
  grid.append(slot, n("div", undefined, "chat-news-reserved"));
  document.querySelector(".overview-stats").after(grid);
  let client,
    account,
    epoch = 0,
    boot = {},
    health = {},
    tab = "global",
    conversation = "",
    folder = "",
    search = "",
    searchTimer,
    reply = null,
    docked = false,
    modeDocked = false,
    modeDockVisible = true,
    loading = false,
    viewGeneration = 0,
    drafts = new Map(),
    observer,
    refreshTimer,
    readTimer,
    readIDs = new Set(),
    seenIDs = new Set(),
    settingsOpen = false,
    unresolved = null,
    entities = [];
  initChatTooltips(() => health);
  const toolbar = Modules.header("chat"),
    heading = toolbar.querySelector("h2"),
    nativeActions = toolbar.querySelector(".window-actions"),
    pinWindow = nativeActions.children[0],
    maxWindow = nativeActions.children[1],
    closeWindow = nativeActions.children[2],
    name = n("div", undefined, "chat-appname"),
    signal = n("span", undefined, "chat-signal down"),
    tools = n("div", undefined, "chat-toolset");
  toolbar.classList.add("chat-titlebar");
  signal.tabIndex = 0;
  name.append(signal, heading);
  toolbar.replaceChildren(name, tools);
  const dock = button(
      "На главную",
      () => setDock(!docked),
      "chat-icon chat-dock",
    ),
    settingsButton = button(
      "⚙",
      () => {
        settingsOpen = !settingsOpen;
        settings.hidden = !settingsOpen;
      },
      "chat-icon",
    );
  settingsButton.setAttribute("aria-label", tr("Настройки чата"));
  const linkButton = button("↗", pickLink, "chat-icon");
  linkButton.title = tr("Ссылка на объект");
  linkButton.setAttribute("aria-label", tr("Ссылка на объект"));
  const searchToggle = button("⌕", () => toggleSearch(), "chat-icon");
  searchToggle.setAttribute("aria-label", tr("Поиск сообщений"));
  searchToggle.setAttribute("aria-expanded", "false");
  maxWindow.classList.add("chat-icon");
  closeWindow.classList.add("chat-icon");
  closeWindow.onclick = () => {
    if (docked) {
      grid.hidden = true;
      document
        .querySelector('#sidebar [data-open="chat"]')
        ?.classList.remove("module-open");
    } else Modules.close("chat");
  };
  tools.append(searchToggle, settingsButton, dock, maxWindow, closeWindow);
  const moderation = button(
    "Модерация",
    () =>
      (boot.user?.role === "admin" || boot.user?.game_moderator
        ? moderationPanel
        : corporateModeration)(client, showError),
    "chat-icon chat-dock",
  );
  moderation.hidden = true;

  const tabbar = n("div", undefined, "chat-tabs");
  tabbar.setAttribute("role", "tablist");
  const tabButtons = new Map();
  for (const [key, title] of tabs) {
    const b = button(title, () => select(key), "chat-tab");
    b.setAttribute("role", "tab");
    tabButtons.set(key, b);
    tabbar.append(b);
  }
  const settings = n("div", undefined, "chat-settings-panel");
  settings.hidden = true;
  pinWindow.textContent = tr("Закрепить окно");
  const opacity = n("input");
  opacity.type = "range";
  opacity.min = 40;
  opacity.max = 100;
  opacity.step = 5;
  opacity.value = 100;
  const opacityLabel = n("label", tr("Прозрачность фона"));
  opacityLabel.append(opacity);
  opacity.oninput = () =>
    root.style.setProperty("--chat-alpha", Number(opacity.value) / 100);
  opacity.onchange = () => saveSettings();
  const sounds = n("input");
  sounds.type = "checkbox";
  const soundsLabel = n("label", tr("Звуковые уведомления"));
  soundsLabel.append(sounds);
  sounds.onchange = () => saveSettings();
  settings.append(
    opacityLabel,
    soundsLabel,
    button("Мои ограничения", async () => {
      const events = await client.get("my.moderation"),
        panel = Modules.create(
          "chat-my-moderation",
          "Мои ограничения",
          500,
          460,
          { private: true },
        );
      panel.replaceChildren();
      for (const e of events) {
        const row = n("article");
        row.append(
          n("b", e.action),
          n("p", e.reason),
          n("time", new Date(e.created_at).toLocaleString(I18n.language)),
        );
        panel.append(row);
      }
      Modules.open("chat-my-moderation");
    }),
  );
  settings.append(linkButton, moderation, pinWindow);
  const searchPanel = n("form", undefined, "chat-search-panel"),
    searchInput = n("input"),
    searchSubmit = button("Сброс", () => {
      searchInput.value = "";
      search = "";
      loadFeed();
    });
  searchPanel.hidden = true;
  searchInput.type = "search";
  searchInput.placeholder = tr("Поиск сообщений");
  searchInput.setAttribute("aria-label", searchInput.placeholder);
  searchSubmit.type = "button";
  searchPanel.append(searchInput, searchSubmit);
  function toggleSearch() {
    searchPanel.hidden = !searchPanel.hidden;
    searchToggle.setAttribute("aria-expanded", String(!searchPanel.hidden));
    searchToggle.classList.toggle("active", !searchPanel.hidden);
    if (!searchPanel.hidden) searchInput.focus();
    else {
      search = "";
      searchInput.value = "";
      loadFeed();
    }
  }
  searchInput.oninput = () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      search = searchInput.value.trim();
      loadFeed();
    }, 220);
  };
  searchPanel.onsubmit = (e) => {
    e.preventDefault();
    search = searchInput.value.trim();
    loadFeed();
  };
  const section = n("div", undefined, "chat-section-header"),
    sectionName = n("span", undefined, "chat-section-name"),
    actions = n("div", undefined, "chat-section-actions");
  section.append(sectionName, actions);
  const folderbar = n("div", undefined, "chat-folder-bar");
  folderbar.hidden = true;
  const restriction = n("div", undefined, "chat-inline-error");
  restriction.hidden = true;
  const area = n("div", undefined, "chat-area"),
    feed = n("div", undefined, "chat-feed");
  area.append(feed);
  area.setAttribute("role", "log");
  const error = n("div", undefined, "chat-inline-error");
  error.setAttribute("role", "status");
  error.hidden = true;
  const compose = n("form", undefined, "chat-compose"),
    replyPreview = n("div", undefined, "chat-reply-preview"),
    composeMain = n("div", undefined, "chat-compose-main"),
    textarea = n("textarea"),
    send = button("Отправить", undefined, "chat-send");
  replyPreview.hidden = true;
  send.type = "submit";
  textarea.placeholder = tr("Сообщение");
  textarea.setAttribute("aria-label", tr("Сообщение"));
  const emoji = button(
    "☺",
    () => emojiPicker(compose, textarea),
    "chat-emoji-trigger",
  );
  emoji.setAttribute("aria-label", tr("Эмодзи"));
  composeMain.append(textarea, emoji, send);
  const meta = n("div", undefined, "chat-compose-meta"),
    hint = n("span", tr("Enter — отправить · Shift+Enter — новая строка")),
    counter = n("span");
  meta.append(hint, counter);
  compose.append(replyPreview, composeMain, meta);
  root.append(
    toolbar,
    tabbar,
    settings,
    searchPanel,
    section,
    restriction,
    folderbar,
    area,
    error,
    compose,
  );
  errorSink = (e) => {
    error.hidden = false;
    error.textContent = tr(
      errors[e.message] || e.message || "Не удалось выполнить действие",
    );
  };
  const key = () =>
    tab +
    ":" +
    (tab === "private" ? conversation : tab === "favorites" ? folder : "");
  const target = () => ({
    kind: tab,
    target:
      tab === "private"
        ? conversation
        : tab === "corporation"
          ? boot.corporation?.[0]?.id
          : tab === "planet"
            ? boot.presence?.[0]?.surface
            : tab === "sector"
              ? boot.presence?.[0]?.surface + "/" + boot.presence?.[0]?.sector
              : "",
  });
  const active = () =>
    Session.signedIn && account === String(Session.data.state?.user.id);
  const visible = () =>
    active() &&
    !document.hidden &&
    root.getBoundingClientRect().bottom > 0 &&
    root.getBoundingClientRect().top < innerHeight &&
    ((docked && !grid.hidden) ||
      (dialog.open && dialog.classList.contains("window-active")));
  function cap() {
    return tab === "favorites" ? 2500 : tab === "private" ? 500 : 256;
  }
  function count() {
    counter.textContent = Array.from(textarea.value).length + " / " + cap();
    send.disabled =
      !client ||
      loading ||
      !!unresolved ||
      !textarea.value.trim() ||
      Array.from(textarea.value).length > cap() ||
      !health.ready ||
      (tab === "private" && !conversation);
  }
  textarea.addEventListener("input", () => {
    drafts.set(key(), { body: textarea.value, entities: [...entities] });
    count();
  });
  textarea.onkeydown = (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      compose.requestSubmit();
    }
  };
  compose.onsubmit = async (e) => {
    e.preventDefault();
    if (send.disabled) return;
    error.hidden = true;
    loading = true;
    count();
    const body = textarea.value,
      scope = target(),
      oldkey = key(),
      id = uuid();
    try {
      await client.command(
        tab === "favorites" ? "favorite.note" : "message.send",
        tab === "favorites"
          ? { body, folder }
          : {
              ...scope,
              body,
              reply_to: reply?.id,
              entities: resolveEntities(body),
            },
        id,
      );
      drafts.delete(oldkey);
      if (key() === oldkey) {
        textarea.value = "";
        entities = [];
        reply = null;
        replyPreview.replaceChildren();
        replyPreview.hidden = true;
      }
      await sync();
    } catch (e) {
      if (e.message === "operation_unknown")
        unresolved = { id, key: oldkey, notBefore: Date.now() + 10000 };
      showError(e);
    } finally {
      loading = false;
      count();
    }
  };
  function resolveEntities(body) {
    const output = [];
    for (const item of entities) {
      const at = body.indexOf(item.text);
      if (at >= 0)
        output.push({
          ...item.entity,
          start: Array.from(body.slice(0, at)).length,
          end: Array.from(body.slice(0, at + item.text.length)).length,
        });
    }
    return output
      .sort((a, b) => a.start - b.start)
      .filter((v, i, a) => !i || v.start >= a[i - 1].end);
  }
  async function command(type, payload) {
    error.hidden = true;
    const result = await client.command(type, payload);
    await sync();
    return result;
  }
  function saveSettings() {
    Preferences.write("chat-settings:" + account, {
      opacity: Number(opacity.value),
      sounds: sounds.checked,
      docked: modeDocked || docked,
      tab,
    });
    return client
      ?.command("settings", {
        settings: {
          opacity: Number(opacity.value),
          sounds: sounds.checked,
          docked: modeDocked || docked,
          tab,
        },
      })
      .catch(showError);
  }
  function setDock(value, persist = true) {
    if (value && document.body.classList.contains("planet-mode")) {
      modeDocked = true;
      modeDockVisible = true;
      value = false;
      persist = false;
    }
    dock.disabled = document.body.classList.contains("planet-mode");
    docked = value;
    maxWindow.hidden = value;
    grid.hidden = !value;
    if (value) {
      slot.append(root);
      Modules.close("chat");
      document.body.classList.add("chat-docked");
      dock.dataset.i18n = "В отдельное окно";
      dock.textContent = tr("В отдельное окно");
    } else {
      content.append(root);
      document.body.classList.remove("chat-docked");
      dock.dataset.i18n = "На главную";
      dock.textContent = tr("На главную");
      Modules.open("chat");
    }
    document
      .querySelector('#sidebar [data-open="chat"]')
      ?.classList.toggle("module-open", value || dialog.open);
    if (persist) saveSettings();
  }
  function select(value) {
    drafts.set(key(), { body: textarea.value, entities: [...entities] });
    tab = value;
    area.scrollTop = 0;
    feed.replaceChildren();
    readIDs.clear();
    delete root.dataset.seen;
    reply = null;
    replyPreview.replaceChildren();
    replyPreview.hidden = true;
    const draft = drafts.get(key());
    textarea.value = draft?.body || "";
    entities = draft?.entities || [];
    for (const [k, b] of tabButtons) {
      b.classList.toggle("active", k === tab);
      b.setAttribute("aria-selected", String(k === tab));
    }
    saveSettings();
    count();
    loadFeed();
  }
  function title() {
    return tab === "private"
      ? boot.conversations?.find((c) => c.id === conversation)?.title ||
          boot.conversations?.find((c) => c.id === conversation)
            ?.participants ||
          tr("Личные беседы")
      : tr(tabs.find((t) => t[0] === tab)?.[1] || "Чат");
  }
  async function sync() {
    if (!client || !active()) return;
    const generation = epoch;
    try {
      const next = await client.get("bootstrap");
      if (generation !== epoch) return;
      boot = next;
      if (!visible()) readIDs.clear();
      await notifications();
      moderation.hidden = !(
        boot.user?.role === "admin" ||
        boot.user?.game_moderator ||
        boot.corporation?.[0]?.owner === String(boot.user?.id) ||
        boot.corporation?.[0]?.permissions?.includes("chat.moderate")
      );
      for (const [k, b] of tabButtons) {
        const label = tr(tabs.find((t) => t[0] === k)[1]),
          fresh = boot.public_new?.some((c) => c.kind === k && c.fresh);
        b.replaceChildren(n("span", label));
        if (fresh) b.append(n("span", undefined, "chat-dot-new"));
        if (k === "private") {
          const unread =
            boot.conversations?.reduce(
              (a, c) => a + Number(c.unread_count),
              0,
            ) || 0;
          if (unread)
            b.append(
              n("span", unread > 99 ? "99+" : String(unread), "chat-count"),
            );
        }
        b.disabled =
          (k === "corporation" && !boot.corporation?.length) ||
          (["planet", "sector"].includes(k) &&
            (!boot.presence?.length || !health.planet));
      }
      if (unresolved) {
        const op = await client.get("operation", { id: unresolved.id });
        if (op.state === "completed") {
          drafts.delete(unresolved.key);
          if (key() === unresolved.key) textarea.value = "";
          unresolved = null;
        } else if (
          op.state === "not_found" &&
          Date.now() > unresolved.notBefore
        ) {
          unresolved = null;
          showError(Error("operation_not_committed"));
        }
      }
      await loadFeed();
    } catch (e) {
      showError(e);
    }
  }
  async function notifications() {
    const notices = await client.get("notifications");
    let sound = false;
    const ids = [];
    for (const item of notices) {
      if (item.seen_at || seenIDs.has(item.id)) continue;
      seenIDs.add(item.id);
      sound = true;
      ids.push(item.id);
    }
    if (sound && sounds.checked) {
      try {
        const ctx = new AudioContext(),
          o = ctx.createOscillator(),
          gain = ctx.createGain();
        o.connect(gain);
        gain.connect(ctx.destination);
        o.frequency.value = 740;
        gain.gain.setValueAtTime(0.025, ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.12);
        o.start();
        o.stop(ctx.currentTime + 0.12);
        o.onended = () => ctx.close();
      } catch {}
    }
    if (ids.length) await client.command("notification.read", { ids });
    while (seenIDs.size > 500) seenIDs.delete(seenIDs.values().next().value);
  }
  function queueSync() {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => sync(), 150);
  }
  async function loadFeed(before = "", append = false) {
    if (!client || !active()) return;
    const generation = ++viewGeneration,
      k = key();
    observer?.disconnect();
    sectionName.textContent = title();
    Modules.title("chat", tr("Чат") + " · " + title());
    restriction.hidden = tab !== "corporation" || !boot.restrictions?.length;
    if (!restriction.hidden) {
      const r = boot.restrictions[0];
      restriction.textContent =
        tr("Отправка ограничена до") +
        " " +
        new Date(r.expires_at).toLocaleString(I18n.language) +
        " · " +
        r.reason;
    }
    folderbar.hidden = tab !== "favorites";
    actions.replaceChildren();
    if (tab !== "favorites") actions.append(button("Игроки", roster));
    error.hidden = true;
    count();
    const oldScroll = area.scrollTop,
      atBottom = area.scrollHeight - area.clientHeight - area.scrollTop < 35;

    if (tab === "private" && !conversation) {
      feed.replaceChildren();
      privateList();
      compose.hidden = true;
      if (search) {
        let found;
        try {
          found = await client.get("private.search", { search });
        } catch (e) {
          showError(e);
          return;
        }
        for (const m of found) {
          const row = button(
            m.author + ": " + m.body,
            () => {
              conversation = m.conversation_id;
              select("private");
            },
            "chat-conversation",
          );
          feed.append(row);
        }
      }
      return;
    }
    compose.hidden = tab === "favorites";
    folderbar.hidden = tab !== "favorites";
    folderbar.replaceChildren();
    if (tab === "favorites") {
      const all = button(
        "Все",
        () => {
          folder = "";
          select("favorites");
        },
        "chat-folder",
      );
      all.classList.toggle("selected", !folder);
      folderbar.append(all);
      for (const f of boot.folders || []) {
        const b = button(
          "▤ " + f.name,
          () => {
            folder = f.id;
            select("favorites");
          },
          "chat-folder",
        );
        b.classList.toggle("selected", folder === f.id);
        folderbar.append(b);
      }
      if (folder)
        folderbar.append(
          button(
            "⋯",
            (event) => {
              const f = boot.folders?.find((f) => f.id === folder);
              if (!f) return;
              UI.menu(
                [
                  {
                    label: "Переименовать",
                    action: () =>
                      ask(
                        "Название папки",
                        [["name", "Название", f.name]],
                        (v) => command("folder.rename", { id: f.id, ...v }),
                      ),
                  },
                  {
                    label: "Удалить папку, сохранить записи",
                    danger: true,
                    action: () =>
                      confirmAction(
                        "Удалить папку, сохранить записи?",
                        async () => {
                          await command("folder.delete", { id: f.id });
                          folder = "";
                          select("favorites");
                        },
                      ),
                  },
                ],
                event.clientX,
                event.clientY,
                { theme: "chat", owner: event.currentTarget },
              );
            },
            "chat-folder chat-folder-options",
          ),
        );
      actions.append(
        button("Новая заметка", () =>
          ask("Заметка", [["body", "Текст"]], (v) =>
            command("favorite.note", { ...v, folder }),
          ),
        ),
        button("Новая папка", () =>
          ask("Новая папка", [["name", "Название"]], (v) =>
            command("folder.create", v),
          ),
        ),
      );
      sectionName.textContent =
        tr("Избранное") +
        " / " +
        (boot.folders?.find((f) => f.id === folder)?.name || tr("Все"));
    }
    if (tab === "private")
      actions.append(
        button("←", () => {
          conversation = "";
          select("private");
        }),
        button("⋯", privateMenu),
      );
    try {
      const params =
        tab === "favorites"
          ? { id: folder, search, before }
          : { ...target(), search, before };
      const rows = await client.get(
        tab === "favorites" ? "favorites" : "history",
        params,
      );
      if (generation !== viewGeneration || k !== key()) return;
      const oldtop = area.scrollTop;
      const rendered = renderMessages(rows, {
        root: feed,
        tab,
        user: boot.user?.id,
        menu: messageMenu,
        mention: mentionUser,
        openLink,
        reply: replyTo,
      });
      if (append) feed.prepend(...rendered);
      else feed.replaceChildren(...rendered);
      if (rows.length === 50) {
        const more = button("Более ранние сообщения", () => {
          more.remove();
          loadFeed(
            tab === "favorites" ? rows.at(-1).id : rows.at(-1).seq,
            true,
          );
        });
        feed.prepend(more);
      }
      if (!append)
        area.scrollTop = search ? 0 : atBottom ? area.scrollHeight : oldScroll;
      else area.scrollTop = oldtop;
      observer = new IntersectionObserver(
        (entries) => {
          if (!visible()) return;
          for (const entry of entries) {
            if (entry.isIntersecting) {
              const row = entry.target;
              if (
                row.dataset.id &&
                tab === "private" &&
                row.dataset.author !== String(boot.user?.id)
              ) {
                readIDs.add(row.dataset.id);
              }
              if (row.dataset.seq) {
                const value = BigInt(row.dataset.seq);
                if (value > BigInt(root.dataset.seen || 0))
                  root.dataset.seen = String(value);
              }
            }
          }
          clearTimeout(readTimer);
          readTimer = setTimeout(markRead, 250);
        },
        { root: area, threshold: 0.1 },
      );
      for (const el of feed.querySelectorAll("[data-id]")) observer.observe(el);
    } catch (e) {
      if (generation === viewGeneration) showError(e);
    }
  }
  async function markRead() {
    if (!visible() || !client) return;
    try {
      if (tab === "private" && readIDs.size) {
        const ids = [...readIDs].slice(0, 100);
        readIDs.clear();
        await client.command("read", { ...target(), ids });
      } else if (!["private", "favorites"].includes(tab) && root.dataset.seen) {
        await client.command("read", {
          ...target(),
          seen_seq: Number(root.dataset.seen),
        });
        delete root.dataset.seen;
      }
    } catch (e) {
      if (e.message !== "chat_unavailable") showError(e);
    }
  }
  function replyTo(m) {
    reply = m;
    replyPreview.hidden = false;
    replyPreview.replaceChildren(
      n("span", m.author + ": " + m.body.slice(0, 80)),
      button("×", () => {
        reply = null;
        replyPreview.replaceChildren();
        replyPreview.hidden = true;
      }),
    );
    textarea.focus();
  }
  function mentionUser(u) {
    const text = "@" + u.name;
    entities.push({ text, entity: { kind: "mention", id: String(u.id) } });
    insert(text + " ");
  }
  function insert(text) {
    textarea.setRangeText(
      text,
      textarea.selectionStart,
      textarea.selectionEnd,
      "end",
    );
    textarea.dispatchEvent(new Event("input"));
    textarea.focus();
  }
  async function pickLink() {
    const panel = Modules.create("chat-link", "Ссылка на объект", 430, 470, {
        private: true,
      }),
      form = n("form"),
      query = n("input"),
      list = n("div");
    query.type = "search";
    query.placeholder = tr("Название объекта");
    const find = button("Поиск");
    find.type = "submit";
    form.append(query, find);
    panel.replaceChildren(form, list);
    async function load() {
      const options = await client.get("link.options", { search: query.value });
      list.replaceChildren();
      for (const option of options)
        list.append(
          button(option.label, async () => {
            const checked = await client.get("link", {
              id: JSON.stringify({ kind: option.kind, id: option.id }),
            });
            const entity = checked[0];
            entities.push({
              text: entity.label,
              entity: { kind: entity.kind, id: entity.id },
            });
            insert(entity.label + " ");
            Modules.close("chat-link");
          }),
        );
    }
    form.onsubmit = (e) => {
      e.preventDefault();
      load().catch(showError);
    };
    Modules.open("chat-link");
    await load();
  }
  async function openLink(entity) {
    const checked = await client.get("link", { id: JSON.stringify(entity) });
    entity = checked[0];
    window.dispatchEvent(new CustomEvent("chatopenlink", { detail: entity }));
    if (entity.kind === "ship") {
      await Game.command({ action: "select_ship", ship: Number(entity.id) });
      Modules.open("ship");
    } else if (entity.kind === "company") Modules.open("corporations");
    else await openPlanet(entity.star, entity.planet);
  }
  function messageMenu(m, event) {
    const items = [];
    if (tab === "favorites") {
      items.push({
        label: "Переместить в папку",
        children: [
          {
            label: "Без папки (Все)",
            checked: !m.folder_id,
            action: () => command("favorite.move", { id: m.id, folder: "" }),
          },
          ...(boot.folders || []).map((f) => ({
            label: f.name,
            checked: m.folder_id === f.id,
            action: () => command("favorite.move", { id: m.id, folder: f.id }),
          })),
        ],
      });
      if (m.kind === "note")
        items.push({
          label: "Редактировать заметку",
          action: () =>
            ask("Заметка", [["body", "Текст", m.body]], (v) =>
              command("favorite.edit", { id: m.id, ...v }),
            ),
        });
      items.push({
        label: "Удалить",
        danger: true,
        action: () =>
          confirmAction("Удалить заметку?", () =>
            command("favorite.delete", { id: m.id }),
          ),
      });
    } else {
      items.push(
        { label: "Ответить", action: () => replyTo(m) },
        {
          label: "В избранное",
          action: () => command("favorite.copy", { id: m.id }),
        },
        {
          label: "Пожаловаться",
          action: () =>
            ask("Жалоба", [["reason", "Причина"]], (v) =>
              command("report.create", { id: m.id, ...v }),
            ),
        },
      );
      if (String(m.author_id) === String(boot.user.id))
        items.push({
          label: "Удалить сообщение",
          danger: true,
          action: () =>
            confirmAction("Удалить сообщение?", () =>
              command("message.delete", { id: m.id }),
            ),
        });
      else if (
        boot.user.role === "admin" ||
        boot.user.game_moderator ||
        (tab === "corporation" &&
          (boot.corporation?.[0]?.owner === String(boot.user.id) ||
            boot.corporation?.[0]?.permissions?.includes("chat.moderate")))
      )
        items.push({
          label: "Удалить как модератор",
          danger: true,
          action: () =>
            ask("Удалить сообщение", [["reason", "Причина"]], (v) =>
              command("message.delete", { id: m.id, ...v }),
            ),
        });
    }
    event.preventDefault();
    UI.menu(items, event.clientX, event.clientY, {
      theme: "chat",
      owner: event.currentTarget,
    });
  }
  function privateList() {
    actions.append(
      button("Новая беседа", () =>
        ask("Новая беседа", [["user", "Никнейм пилота"]], async (v) => {
          const r = await command("private.create", { kind: "dm", ...v });
          conversation = r.conversation;
          select("private");
        }),
      ),
      button("Группа", () =>
        ask("Новая группа", [["title", "Название"]], async (v) => {
          const r = await command("private.create", { kind: "group", ...v });
          conversation = r.conversation;
          select("private");
        }),
      ),
    );
    for (const invite of boot.invitations || []) {
      const row = n("article", undefined, "chat-invite");
      row.append(
        n("b", invite.title || invite.sender),
        n("p", tr("Приглашение от") + " " + invite.sender),
        button("Принять", () =>
          command("private.decide", { id: invite.id, decision: "accept" }),
        ),
        button("Отклонить", () =>
          command("private.decide", { id: invite.id, decision: "reject" }),
        ),
      );
      feed.append(row);
    }
    for (const c of boot.conversations || []) {
      const row = button(
        (c.type === "group" ? "⬡ " : "◌ ") +
          (c.title || c.participants || tr("Новая беседа")) +
          (c.unread_count ? " · " + c.unread_count : "") +
          (c.state === "pending" ? " · " + tr("Ожидание") : "") +
          (c.state === "deleting" ? " · " + tr("Удаляется") : ""),
        () => {
          conversation = c.id;
          select("private");
        },
        "chat-conversation",
      );
      row.disabled = c.state === "deleting";
      feed.append(row);
    }
    if (!feed.childElementCount)
      feed.append(
        n("p", tr("Здесь появятся ваши личные беседы"), "chat-empty"),
      );
  }
  function privateMenu(event) {
    const c = boot.conversations?.find((c) => c.id === conversation);
    if (!c) return;
    const items = [];
    if (c.owner === String(boot.user.id)) {
      items.push({
        label: "Пригласить пилота",
        action: () =>
          ask("Пригласить пилота", [["user", "Никнейм пилота"]], (v) =>
            command("private.invite", { target: conversation, ...v }),
          ),
      });
      if (c.type === "group")
        items.push({
          label: "Переименовать",
          action: () =>
            ask("Название группы", [["title", "Название", c.title]], (v) =>
              command("private.rename", { target: conversation, ...v }),
            ),
        });
      items.push({
        label: "Удалить беседу",
        danger: true,
        action: () =>
          confirmAction("Удалить беседу и все её сообщения?", async () => {
            await command("private.delete", {
              target: conversation,
              confirm: true,
            });
            conversation = "";
            select("private");
          }),
      });
    }
    items.push({
      label: "Покинуть беседу",
      action: () =>
        ask(
          "Покинуть беседу",
          [["user", "Новый владелец (если нужен)"]],
          async (v) => {
            await command("private.leave", { target: conversation, ...v });
            conversation = "";
            select("private");
          },
        ),
    });
    UI.menu(items, event.clientX, event.clientY, {
      theme: "chat",
      owner: event.currentTarget,
    });
  }
  async function roster() {
    const rows = await client.get(
        "roster",
        tab === "favorites" || (tab === "private" && !conversation)
          ? { kind: "global" }
          : target(),
      ),
      panel = Modules.create("chat-roster", "Пилоты в чате", 420, 540, {
        private: true,
      });
    panel.classList.add("chat-players-content");
    const summary = n("div", undefined, "chat-players-summary"),
      filter = n("div", undefined, "chat-players-filter"),
      search = n("input"),
      list = n("div", undefined, "chat-players-list"),
      count = n("span", String(rows.length), "chat-players-total");
    summary.append(n("span", title()), count);
    search.type = "search";
    search.placeholder = tr("Поиск игроков");
    search.setAttribute("aria-label", search.placeholder);
    filter.append(search);
    panel.replaceChildren(summary, filter, list);
    search.oninput = () => {
      let visible = 0;
      for (const row of list.children) {
        row.hidden = !row.dataset.search.includes(
          search.value.trim().toLocaleLowerCase(),
        );
        if (!row.hidden) visible++;
      }
      count.textContent = String(visible);
    };
    if (["private", "corporation"].includes(tab))
      panel.append(
        button("Упомянуть всех", () => {
          entities.push({ text: "@all", entity: { kind: "all", id: "all" } });
          insert("@all ");
        }),
      );
    for (const u of rows) {
      const row = n("div", undefined, "chat-player-row with-manage");
      row.dataset.search = (u.name + " " + (u.tag || "")).toLocaleLowerCase();
      const status = n(
          "span",
          undefined,
          "chat-player-presence" + (u.online ? "" : " offline"),
        ),
        info = n("div", undefined, "chat-player-name"),
        role = n(
          "span",
          u.owner ? tr("Владелец") : "",
          "chat-player-role" + (u.owner ? " owner" : ""),
        );
      status.dataset.help = u.online ? "Онлайн" : "Недоступна";
      info.append(
        n("strong", u.name),
        n("span", u.tag ? "[" + u.tag + "]" : ""),
      );
      info.onclick = () => mentionUser(u);
      row.append(
        status,
        n("span", u.name.slice(0, 1).toUpperCase(), "chat-player-avatar"),
        info,
        role,
      );
      row.append(
        button("⋯", (event) => {
          const items = [
            { label: "Упомянуть", action: () => mentionUser(u) },
            {
              label: "Личное сообщение",
              action: async () => {
                const r = await command("private.create", {
                  kind: "dm",
                  user: u.id,
                });
                conversation = r.conversation;
                select("private");
              },
            },
          ];
          const c = boot.conversations?.find((c) => c.id === conversation);
          if (
            tab === "private" &&
            c?.type === "group" &&
            c.owner === String(boot.user.id) &&
            u.id !== String(boot.user.id)
          )
            items.push(
              {
                label: "Передать владельца",
                action: () =>
                  command("private.owner", {
                    target: conversation,
                    user: u.id,
                    revision: Number(c.revision),
                  }),
              },
              {
                label: "Исключить",
                action: () =>
                  command("private.kick", { target: conversation, user: u.id }),
              },
            );
          if (
            tab === "corporation" &&
            (boot.user?.role === "admin" ||
              boot.user?.game_moderator ||
              (u.id !== String(boot.user.id) &&
                (boot.corporation?.[0]?.owner === String(boot.user.id) ||
                  (boot.corporation?.[0]?.permissions?.includes(
                    "chat.moderate",
                  ) &&
                    !u.owner &&
                    !u.permissions?.includes("chat.moderate")))))
          )
            items.push(
              {
                label: "Ограничить отправку сообщений",
                children: [300, 1800, 3600, 86400, 604800].map((duration) => ({
                  label:
                    duration < 3600
                      ? duration / 60 + " " + tr("мин")
                      : duration / 3600 + " " + tr("ч"),
                  action: () =>
                    ask("Ограничение чата", [["reason", "Причина"]], (v) =>
                      command("mute", {
                        ...target(),
                        user: u.id,
                        duration,
                        ...v,
                      }),
                    ),
                })),
              },
              {
                label: "Снять ограничение",
                action: () =>
                  command("unmute", {
                    ...target(),
                    user: u.id,
                    reason: tr("Ограничение снято модератором"),
                  }),
              },
            );
          UI.menu(items, event.clientX, event.clientY, {
            theme: "chat",
            owner: event.currentTarget,
          });
        }),
      );
      list.append(row);
    }
    Modules.open("chat-roster");
  }
  async function confirmAction(text, action) {
    UI.confirm(tr("Подтверждение"), tr(text), async () => {
      await action();
      return true;
    });
  }
  function ask(title, fields, submit) {
    const panel = Modules.create("chat-form", title, 420, 350, {
      private: true,
    });
    const form = n("form"),
      inline = n("p", undefined, "chat-inline-error");
    inline.setAttribute("role", "alert");
    const controls = {};
    for (const [key, label, value] of fields) {
      const el = n(key === "body" ? "textarea" : "input");
      el.value = value || "";
      el.maxLength = key === "body" ? 2500 : 200;
      const l = n("label", tr(label));
      l.append(el);
      form.append(l);
      controls[key] = el;
      el.addEventListener("input", () => {
        el.removeAttribute("aria-invalid");
        inline.textContent = "";
      });
    }
    const b = button("Сохранить");
    b.type = "submit";
    form.append(inline, b);
    form.onsubmit = async (e) => {
      e.preventDefault();
      b.disabled = true;
      try {
        await submit(
          Object.fromEntries(
            Object.entries(controls).map(([k, v]) => [k, v.value]),
          ),
        );
        Modules.close("chat-form");
      } catch (e) {
        inline.textContent = tr(errors[e.message] || e.message);
        for (const field of Object.values(controls))
          field.setAttribute("aria-invalid", "true");
      } finally {
        b.disabled = false;
      }
    };
    panel.replaceChildren(form);
    Modules.open("chat-form");
    form.querySelector("input,textarea")?.focus();
  }
  function updateHealth(value) {
    const recover =
      (!health.ready && value?.ready) || (!health.planet && value?.planet);
    health = value || {};
    if (recover && client) queueSync();
    signal.className =
      "chat-signal " +
      (health.ready && health.cosmic && health.planet
        ? "healthy"
        : !!health.cosmic !== !!health.planet
          ? "partial"
          : "down");
    signal.dataset.help =
      tr("Статус служб") +
      "\n■ Cosmic: " +
      (health.cosmic ? tr("Онлайн") : tr("Недоступна")) +
      "\n■ Planet: " +
      (health.planet ? tr("Онлайн") : tr("Недоступна"));
    count();
  }
  function start() {
    const id = Session.signedIn && Session.data.state?.user.id;
    if (Session.data.state?.chat_enabled !== true) return;
    if (!id) return;
    if (account === String(id)) return;
    stop();
    account = String(id);
    epoch++;
    client = new ChatClient((event) => {
      if (event.health) updateHealth(event.health);
      if (event.type === "hello") {
        const currentEpoch = epoch;
        client
          .get("bootstrap")
          .then((initial) => {
            if (epoch !== currentEpoch) return;
            boot = initial;
            const saved = Preferences.read(
              "chat-settings:" + account,
              initial.settings || {},
            );
            tab = saved.tab || "global";
            opacity.value = saved.opacity || 100;
            sounds.checked = !!saved.sounds;
            root.style.setProperty("--chat-alpha", Number(opacity.value) / 100);
            if (saved.docked) setDock(true);
            select(tab);
            sync();
          })
          .catch((e) => {
            if (epoch === currentEpoch) showError(e);
          });
      }
      if (event.type === "sync") queueSync();
      if (event.type === "offline") {
        updateHealth({});
        showError(Error(event.error));
      }
    });
    client.connect();
  }
  function stop() {
    viewGeneration++;
    epoch++;
    client?.close();
    client = null;
    account = null;
    observer?.disconnect();
    clearTimeout(refreshTimer);
    clearTimeout(readTimer);
    clearTimeout(searchTimer);
    drafts.clear();
    root.querySelector(".chat-emoji-panel")?.remove();
    readIDs.clear();
    seenIDs.clear();
    unresolved = null;
    boot = {};
    textarea.value = "";
    entities = [];
    conversation = "";
    folder = "";
    search = "";
    feed.replaceChildren();
    grid.hidden = true;
    content.append(root);
    docked = false;
    modeDocked = false;
    dock.disabled = false;
    document.body.classList.remove("chat-docked");
  }
  window.addEventListener("gamedata", start);
  window.addEventListener("signedout", stop);
  window.addEventListener("sessionexpired", stop);
  // Move the same reader/composer into its existing window while the home
  // workspace is suspended. Keep the user's dock preference and transport.
  window.addEventListener("planetmodechange", ({ detail: active }) => {
    dock.disabled = active;
    if (active && docked) {
      modeDocked = true;
      modeDockVisible = !grid.hidden;
      setDock(false, false);
      if (!modeDockVisible) Modules.close("chat");
    } else if (!active && modeDocked) {
      modeDocked = false;
      setDock(true, false);
      if (!modeDockVisible) grid.hidden = true;
    }
  });
  window.addEventListener("moduleopen", ({ detail }) => {
    if (detail === "chat") {
      start();
      if (docked) {
        grid.hidden = false;
        slot.scrollIntoView({ behavior: "smooth", block: "center" });
        Modules.close("chat");
        document
          .querySelector('#sidebar [data-open="chat"]')
          ?.classList.add("module-open");
      } else sync();
    }
  });
  window.addEventListener("languagechange", () => {
    for (const [key, b] of tabButtons)
      b.textContent = tr(tabs.find((t) => t[0] === key)[1]);
    if (client) loadFeed();
  });
  document.addEventListener("visibilitychange", () => {
    if (visible()) loadFeed();
  });
  window.addEventListener("chatinsertlink", ({ detail }) => {
    if (!client) return;
    const text = detail.label || detail.id;
    entities.push({ text, entity: detail });
    insert(text + " ");
  });
  start();
}
