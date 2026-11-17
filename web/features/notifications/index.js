import { loadPlanet } from "../planet/loader.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { refresh } from "../../core/snapshot-store.js";
import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";
import { world } from "../../core/world-store.js";
import { res, fmtTime } from "../selectors.js";
import {
  notificationCategory,
  categoryNames,
  notificationIcon,
  notificationTime,
} from "./presentation.js";

export function createNotifications({ roots, command }) {
  const n = UI.node,
    tr = (s, params) => I18n.t(s, params),
    b = UI.button;
  const root = roots.notifications;
  let selected = new Set(),
    filter = "all",
    selecting = false,
    pending = false,
    noticeCursor = null,
    loadGeneration = 0,
    listKey = "",
    nodes = null,
    detailID = null,
    returnFocus = null,
    detailKey = "";
  const eventNames = {
    arrival: "Корабль прибыл",
    landing_ready: "Посадочная площадка готова",
    mining_complete: "Добыча завершена",
    research_complete: "Исследование завершено",
    delivery_complete: "Доставка завершена",
    company_invitation: "Приглашение в корпорацию",
    project_complete: "Проект завершён",
    portal_open: "Портал активирован",
    colony: "Колонизация завершена",
    message: "Системное уведомление",
  };
  function eventText(item) {
    const data = item.data || {};
    if (item.kind === "landing_ready")
      return (
        tr("Посадочная площадка готова") +
        " · " +
        data.planet +
        " · " +
        tr("Сектор") +
        " " +
        data.sector
      );
    if (item.kind === "colony")
      return (
        tr("Колонизация завершена") +
        " · " +
        data.planet +
        " · " +
        tr("Сектор") +
        " " +
        data.sector
      );
    return (
      (item.message
        ? tr(item.message) +
          (data.ship_name ? " · " + data.ship_name : "") +
          (data.reason ? " · " + tr(data.reason) : "")
        : "") ||
      tr(
        Object.hasOwn(eventNames, item.kind)
          ? eventNames[item.kind]
          : "Системное уведомление",
      ) +
        (data.star
          ? " · " +
            (world.data?.stars.find((s) => s.id === data.star)?.name ||
              "#" + data.star)
          : "") +
        (data.resource
          ? " · " + tr(res(data.resource)?.name || data.resource)
          : "") +
        (data.quantity ? " × " + data.quantity : "")
    );
  }

  const info = () =>
    Session.data.notifications || { items: [], count: 0, unread: 0 };
  const items = () => (Array.isArray(info().items) ? info().items : []);
  const visibleItems = () =>
    items().filter((item) => filter === "all" || !item.seen);
  const unread = () => Math.max(0, Number(info().unread) || 0);
  const current = (epoch) => Session.signedIn && epoch === Session.epoch;
  const label = (item) =>
    tr(
      Object.hasOwn(eventNames, item.kind)
        ? eventNames[item.kind]
        : "Системное уведомление",
    );
  function countText(element) {
    element.textContent = unread() > 99 ? "99+" : String(unread());
    element.hidden = unread() === 0;
    element.setAttribute(
      "aria-label",
      tr("Непрочитанных: {count}", { count: unread() }),
    );
  }
  async function loadNotifications({ allowPending = false } = {}) {
    if (!Session.signedIn || (pending && !allowPending)) return;
    const epoch = Session.epoch,
      generation = ++loadGeneration;
    const data = await api("notifications");
    if (!current(epoch) || generation !== loadGeneration) return;
    Session.data.notifications = data;
    if (!pending) {
      notificationBadge();
      if (Modules.isOpen("notifications")) renderNotifications();
    }
  }
  function notificationBadge() {
    if (pending || !Session.signedIn) return;
    const badge = document.getElementById("notification-count");
    if (badge) countText(badge);
    const titleCount = Modules.header("notifications")?.querySelector(
      ".notification-compact-count",
    );
    if (titleCount) countText(titleCount);
    const latest = items()[0]?.id || 0;
    if (noticeCursor !== null) {
      const fresh = items().filter(
        (item) => item.id > noticeCursor && item.kind !== "message",
      );
      if (fresh.length) notify(eventText(fresh[0]), fresh[0].level, false);
    }
    // Deleting the latest entry must not make an older saved entry look new.
    noticeCursor = Math.max(noticeCursor || 0, latest);
  }
  async function mutate(body) {
    if (pending || !Session.signedIn) return false;
    const epoch = Session.epoch;
    pending = true;
    ++loadGeneration; // Discard any earlier standalone notification read.
    renderNotifications();
    try {
      const data = await api("notifications", body);
      if (!current(epoch)) return false;
      Session.data.notifications = data;
      selected.clear();
      // Drain a snapshot that started before this write, then take a fresh one.
      // It must not restore an unread/deleted entry after the mutation succeeds.
      await refresh(true);
      if (!current(epoch)) return false;
      await loadNotifications({ allowPending: true });
      return true;
    } catch (error) {
      if (current(epoch)) notify(error.message, "error");
      return false;
    } finally {
      if (current(epoch)) {
        pending = false;
        notificationBadge();
        renderNotifications();
      }
    }
  }
  function closeDetail(restore = true) {
    detailID = null;
    detailKey = "";
    if (!nodes) return;
    nodes.detail.hidden = true;
    nodes.body.inert = false;
    nodes.detail.replaceChildren();
    if (restore && Modules.isOpen("notifications")) {
      const row =
        returnFocus &&
        [...nodes.list.querySelectorAll(".notification-compact-open")].find(
          (button) => button.dataset.noticeId === String(returnFocus),
        );
      (row || nodes.scroll).focus({ preventScroll: true });
    }
    returnFocus = null;
  }
  function detailKeys(event) {
    if (detailID === null || !nodes || nodes.detail.hidden) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closeDetail();
      return;
    }
    if (event.key !== "Tab") return;
    const buttons = [...nodes.detail.querySelectorAll("button:not(:disabled)")];
    if (!buttons.length) return;
    if (event.shiftKey && document.activeElement === buttons[0]) {
      event.preventDefault();
      buttons.at(-1).focus();
    } else if (!event.shiftKey && document.activeElement === buttons.at(-1)) {
      event.preventDefault();
      buttons[0].focus();
    }
  }
  function layout() {
    const bar = Modules.header("notifications");
    if (!bar.querySelector(".notification-compact-appname")) {
      const heading = bar.querySelector("h2"),
        name = n("div", undefined, "notification-compact-appname"),
        led = n("span", undefined, "notification-compact-led"),
        counter = n("span", undefined, "notification-compact-count");
      led.setAttribute("aria-hidden", "true");
      bar.insertBefore(name, bar.firstChild);
      name.append(led, heading, counter);
    }
    if (nodes && root.contains(nodes.body)) return;
    root.replaceChildren();
    listKey = "";
    const body = n("div", undefined, "notification-compact-body"),
      status = n("div", undefined, "notification-compact-statusbar"),
      statusText = n("span"),
      statusTools = n("div", undefined, "notification-compact-status-tools"),
      total = n("span", undefined, "notification-compact-statusbar-mark"),
      menu = b("⋯", (event) => showMenu(event.currentTarget));
    menu.classList.add("notification-compact-menu");
    menu.setAttribute("aria-label", tr("Действия с уведомлениями"));
    menu.setAttribute("aria-haspopup", "menu");
    statusTools.append(total, menu);
    status.append(statusText, statusTools);
    const selection = n("div", undefined, "notification-compact-selection"),
      all = n("input"),
      chosen = n("span"),
      done = b("Готово", () => {
        selecting = false;
        selected.clear();
        renderNotifications();
      });
    all.type = "checkbox";
    all.setAttribute("aria-label", tr("Выбрать все показанные"));
    all.onchange = () => {
      for (const item of visibleItems())
        all.checked ? selected.add(item.id) : selected.delete(item.id);
      renderNotifications();
    };
    selection.append(all, chosen, done);
    const scroll = n("div", undefined, "notification-compact-scroll"),
      list = n("div", undefined, "notification-compact-list"),
      limit = n("p", undefined, "notification-compact-limit"),
      footer = n("div", undefined, "notification-compact-footer"),
      filters = n("div", undefined, "notification-compact-filter");
    scroll.tabIndex = 0;
    scroll.setAttribute("role", "region");
    scroll.setAttribute("aria-label", tr("Список уведомлений"));
    scroll.append(list, limit);
    const buttons = {};
    for (const [value, text] of [
      ["all", "Все"],
      ["unread", "Непрочитанные"],
    ]) {
      const button = b(text, () => {
        filter = value;
        renderNotifications();
        scroll.scrollTop = 0;
      });
      button.classList.add("notification-compact-filter-choice");
      button.dataset.filter = value;
      filters.append(button);
      buttons[value] = button;
    }
    filters.setAttribute("role", "group");
    filters.setAttribute("aria-label", tr("Фильтр уведомлений"));
    const markAll = b("Прочитать всё", () =>
      mutate({ action: "seen", ids: [] }),
    );
    markAll.classList.add("notification-compact-mark-all");
    const tick = n("span", "✓");
    tick.setAttribute("aria-hidden", "true");
    markAll.prepend(tick);
    markAll.setAttribute("aria-label", tr("Прочитать всё"));
    footer.append(filters, markAll);
    body.append(status, selection, scroll, footer);
    const detail = n("div", undefined, "notification-compact-detail-backdrop");
    detail.hidden = true;
    detail.onclick = (event) => {
      if (event.target === detail) closeDetail();
    };
    root.append(body, detail);
    nodes = {
      body,
      statusText,
      total,
      menu,
      selection,
      all,
      chosen,
      done,
      scroll,
      list,
      limit,
      buttons,
      markAll,
      detail,
    };
    bar
      .closest("dialog")
      .addEventListener("keydown", detailKeys, { capture: true });
  }
  function showMenu(owner, item = null) {
    const entries = item
      ? [
          {
            label: "Отметить прочитанным",
            disabled: pending || !!item.seen,
            action: () => mutate({ action: "seen", ids: [item.id] }),
          },
          { separator: true },
          {
            label: "Удалить уведомление",
            danger: true,
            disabled: pending,
            action: () => mutate({ action: "delete", ids: [item.id] }),
          },
        ]
      : [
          {
            label: selecting ? "Завершить выбор" : "Выбрать уведомления",
            disabled: pending,
            action: () => {
              selecting = !selecting;
              if (!selecting) selected.clear();
              renderNotifications();
            },
          },
          {
            label: "Отметить выбранные прочитанными",
            disabled: pending || !selected.size,
            action: () => mutate({ action: "seen", ids: [...selected] }),
          },
          {
            label: "Удалить выбранные",
            disabled: pending || !selected.size,
            danger: true,
            action: () => mutate({ action: "delete", ids: [...selected] }),
          },
          { separator: true },
          {
            label: "Очистить все",
            disabled: pending || !Number(info().count),
            danger: true,
            action: () => mutate({ action: "clear" }),
          },
        ];
    const box = owner.getBoundingClientRect();
    UI.menu(entries, box.left, box.bottom + 3, { owner, theme: "chat" });
  }
  function row(item) {
    const category = notificationCategory(item),
      article = n("article", undefined, "notification-compact-row"),
      glyph = n("div", undefined, "notification-compact-glyph"),
      text = b("", () => openDetail(item.id));
    article.dataset.id = String(item.id);
    article.dataset.kind = category;
    article.dataset.level = item.level;
    article.dataset.read = String(!!item.seen);
    if (selecting) {
      const check = n("input");
      check.type = "checkbox";
      check.checked = selected.has(item.id);
      check.disabled = pending;
      check.setAttribute(
        "aria-label",
        tr("Выбрать уведомление: {title}", { title: label(item) }),
      );
      check.onchange = () => {
        check.checked ? selected.add(item.id) : selected.delete(item.id);
        renderNotifications();
      };
      glyph.append(check);
    } else glyph.append(notificationIcon(category));
    text.classList.add("notification-compact-open");
    text.dataset.noticeId = String(item.id);
    text.setAttribute(
      "aria-label",
      tr("Открыть уведомление: {title}", { title: label(item) }),
    );
    const meta = n("span", undefined, "notification-compact-row-meta"),
      time = n(
        "time",
        notificationTime(item.created_at),
        "notification-compact-time",
      );
    time.dateTime = item.created_at;
    time.title = fmtTime(item.created_at);
    meta.append(
      n("span", tr(categoryNames[category]), "notification-compact-kind"),
      time,
    );
    text.append(
      meta,
      n("span", label(item), "notification-compact-row-title"),
      n("span", eventText(item), "notification-compact-row-excerpt"),
    );
    const end = n("div", undefined, "notification-compact-row-end");
    if (!item.seen) {
      const dot = n("span", undefined, "notification-compact-unread-dot");
      dot.setAttribute("aria-label", tr("Непрочитанное"));
      const read = b(
        "✓",
        () => mutate({ action: "seen", ids: [item.id] }),
        pending,
      );
      read.classList.add("notification-compact-mark-one");
      read.setAttribute(
        "aria-label",
        tr("Отметить прочитанным: {title}", { title: label(item) }),
      );
      end.append(dot, read);
    } else end.append(n("span", "✓", "notification-compact-read-mark"));
    article.oncontextmenu = (event) => {
      event.preventDefault();
      showMenu(text, item);
    };
    article.append(glyph, text, end);
    return article;
  }
  function openDetail(id) {
    if (!items().some((item) => item.id === id)) return;
    returnFocus = id;
    detailID = id;
    detailKey = "";
    renderDetail();
    nodes.detail.querySelector("button")?.focus({ preventScroll: true });
  }
  function renderDetail() {
    if (detailID === null || !nodes) return;
    const item = items().find((item) => item.id === detailID);
    if (!item) {
      closeDetail();
      return;
    }
    const key = JSON.stringify([item, I18n.language, pending]);
    if (key === detailKey && !nodes.detail.hidden) return;
    detailKey = key;
    const hadFocus = nodes.detail.contains(document.activeElement);
    const category = notificationCategory(item),
      panel = n("section", undefined, "notification-compact-detail-panel"),
      heading = n("div", undefined, "notification-compact-detail-heading"),
      close = b("×", () => closeDetail()),
      title = n("h3", label(item)),
      text = n("div", eventText(item), "notification-compact-detail-text"),
      time = n("time", fmtTime(item.created_at));
    title.id = "notification-compact-detail-title";
    text.id = "notification-compact-detail-text";
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    panel.setAttribute("aria-labelledby", title.id);
    panel.setAttribute("aria-describedby", text.id);
    const kind = n(
      "span",
      tr(categoryNames[category]),
      "notification-compact-detail-category",
    );
    kind.dataset.kind = category;
    close.classList.add("notification-compact-detail-close");
    close.setAttribute("aria-label", tr("Закрыть подробности"));
    heading.append(kind, close);
    time.dateTime = item.created_at;
    const actions = n("div", undefined, "notification-compact-detail-actions");
    const action = (label, run, disabled = pending) =>
      actions.append(b(label, run, disabled));
    action(
      "Отметить прочитанным",
      () => mutate({ action: "seen", ids: [item.id] }),
      pending || !!item.seen,
    );
    if (
      item.kind === "landing_ready" &&
      item.data?.star &&
      item.data?.planet_index
    )
      action("Открыть поверхность", () =>
        loadPlanet().then((planet) =>
          planet.open(item.data.star, item.data.planet_index),
        ),
      );
    if (item.kind === "colony" && item.data?.project)
      action("Открыть проект", () =>
        loadPlanet().then((planet) => planet.project(item.data.project)),
      );
    if (item.data?.star)
      action("На карте", () => {
        Modules.open("galaxy");
        window.dispatchEvent(
          new CustomEvent("showstar", { detail: item.data.star }),
        );
      });
    if (item.data?.ship)
      action("Корабль", async () => {
        if (await command({ action: "select_ship", ship: item.data.ship }))
          Modules.open("ship");
      });
    if (item.kind === "mining_complete")
      action("Трюм", () => Modules.open("cargo"));
    action("Удалить уведомление", () =>
      mutate({ action: "delete", ids: [item.id] }),
    );
    action("Закрыть", () => closeDetail(), false);
    panel.append(heading, title, time, text, actions);
    nodes.detail.replaceChildren(panel);
    nodes.detail.hidden = false;
    nodes.body.inert = true;
    if (hadFocus) close.focus({ preventScroll: true });
  }
  function renderNotifications() {
    if (!Session.signedIn || !Modules.isOpen("notifications")) return;
    layout();
    if (pending && nodes.list.childElementCount) {
      for (const button of nodes.body.querySelectorAll("button,input"))
        button.disabled = true;
      nodes.statusText.textContent = tr("Сохраняем…");
      renderDetail();
      return;
    }
    for (const control of nodes.list.querySelectorAll("button,input"))
      control.disabled = false;
    const data = info(),
      loaded = items(),
      visible = visibleItems();
    selected = new Set(
      [...selected].filter((id) => loaded.some((item) => item.id === id)),
    );
    countText(
      Modules.header("notifications").querySelector(
        ".notification-compact-count",
      ),
    );
    nodes.statusText.textContent = tr(
      filter === "unread" ? "Непрочитанные" : "Лента событий",
    );
    nodes.total.textContent =
      "● " + tr("{count} событий", { count: data.count || 0 });
    nodes.selection.hidden = !selecting;
    nodes.chosen.textContent = tr("Выбрано: {count}", { count: selected.size });
    nodes.all.checked =
      visible.length > 0 && visible.every((item) => selected.has(item.id));
    nodes.all.indeterminate =
      visible.some((item) => selected.has(item.id)) && !nodes.all.checked;
    nodes.all.disabled = pending || !visible.length;
    nodes.done.disabled = pending;
    nodes.menu.disabled = pending;
    nodes.markAll.disabled = pending || !unread();
    for (const [value, button] of Object.entries(nodes.buttons)) {
      button.disabled = pending;
      button.classList.toggle("active", filter === value);
      button.setAttribute("aria-pressed", String(filter === value));
    }
    nodes.limit.hidden = Number(data.count) <= loaded.length;
    nodes.limit.textContent = tr(
      "Показаны последние {loaded} из {count} уведомлений.",
      { loaded: loaded.length, count: data.count || 0 },
    );
    const key = JSON.stringify([
      data,
      filter,
      selecting,
      [...selected],
      I18n.language,
      Math.floor(Date.now() / 60000),
    ]);
    if (key !== listKey) {
      listKey = key;
      const top = nodes.scroll.scrollTop,
        focus = document.activeElement?.closest("[data-id]")?.dataset.id;
      const fragment = document.createDocumentFragment();
      if (!visible.length) {
        const empty = n("div", undefined, "notification-compact-empty");
        empty.append(
          n("span", "◇", "notification-compact-empty-symbol"),
          n(
            "strong",
            tr(
              unread() > 0
                ? "Есть более ранние непрочитанные уведомления"
                : loaded.length
                  ? "Новых уведомлений нет"
                  : "Всё спокойно",
            ),
          ),
          n(
            "p",
            tr(
              unread() > 0
                ? "Они находятся за пределами загруженных записей. Кнопка «Прочитать всё» отметит все уведомления на сервере."
                : "Новые события появятся здесь.",
            ),
          ),
        );
        fragment.append(empty);
      } else
        for (const [name, group] of [
          ["Непрочитанные", visible.filter((item) => !item.seen)],
          ["Прочитанные", visible.filter((item) => item.seen)],
        ]) {
          if (!group.length) continue;
          const section = n("section", undefined, "notification-compact-group"),
            heading = n("h3", undefined, "notification-compact-group-heading");
          heading.append(n("span", tr(name)), n("small", String(group.length)));
          section.setAttribute("aria-label", tr(name));
          section.append(heading, ...group.map(row));
          fragment.append(section);
        }
      nodes.list.replaceChildren(fragment);
      nodes.scroll.scrollTop = top;
      if (focus && detailID === null) {
        const target = [...nodes.list.querySelectorAll("[data-id]")].find(
          (node) => node.dataset.id === focus,
        );
        (selecting
          ? target?.querySelector("input")
          : target?.querySelector(".notification-compact-open")
        )?.focus({ preventScroll: true });
      }
    }
    renderDetail();
  }
  function clear() {
    ++loadGeneration;
    selected.clear();
    filter = "all";
    selecting = false;
    pending = false;
    noticeCursor = null;
    listKey = "";
    closeDetail(false);
    root.replaceChildren();
    nodes = null;
    for (const badge of [
      document.getElementById("notification-count"),
      Modules.header("notifications")?.querySelector(
        ".notification-compact-count",
      ),
    ])
      if (badge) {
        badge.hidden = true;
        badge.textContent = "";
      }
  }
  window.addEventListener("moduleclose", (event) => {
    if (event.detail === "notifications") closeDetail(false);
  });
  window.addEventListener("languagechange", () => {
    const id = detailID;
    closeDetail(false);
    root.replaceChildren();
    nodes = null;
    listKey = "";
    if (Modules.isOpen("notifications")) {
      renderNotifications();
      if (id !== null) openDetail(id);
    }
  });
  return {
    eventText,
    loadNotifications,
    notificationBadge,
    renderNotifications,
    clear,
  };
}
