import { pager } from "../../ui/pager.js";
import { RequestScope, Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";

const categories = {
  gameplay: "Игровой процесс",
  interface: "Интерфейс",
  economy: "Экономика",
  other: "Другое",
};
const statuses = {
  pending: "На рассмотрении",
  approved: "Одобрено",
  rejected: "Не одобрено",
};
const tabs = [
  ["community", "Идеи сообщества"],
  ["mine", "Мои идеи"],
  ["form", "Предложить идею"],
];
const requestKey = () =>
  [...crypto.getRandomValues(new Uint32Array(4))]
    .map((v) => v.toString(16).padStart(8, "0"))
    .join("");
const blankDraft = () => ({
  title: "",
  description: "",
  category: "gameplay",
  request: requestKey(),
});
const count = (s) => Array.from(s.trim()).length;
const tr = (s, parameters) => I18n.t(s, parameters);

export function initIdeas() {
  const n = UI.node,
    b = UI.button;
  const player = Modules.create("ideas", "Предложить идею", 1000, 720);
  const review = Modules.create("adminideas", "Предложения игроков", 1060, 720, {
    audience: "admin",
  });
  player.classList.add("ideas-content", "ideas-player");
  review.classList.add("ideas-content");

  const admin = () =>
    Session.signedIn && Session.data.state?.user.role === "admin";
  const active = (scope) =>
    scope === "all"
      ? admin() && Modules.isOpen("adminideas")
      : Session.signedIn && Modules.isOpen("ideas") && tab === scope;
  const initialView = () => ({
    page: 1,
    sort: "newest",
    search: "",
    category: "",
    selected: null,
    mobileDetail: false,
    scroll: 0,
    data: null,
    error: "",
    loading: false,
    loaded: 0,
  });
  const views = Object.fromEntries(
    ["community", "mine", "all"].map((scope) => [
      scope,
      { ...initialView(), requests: new RequestScope(() => active(scope)) },
    ]),
  );
  const panels = {};
  let tab = "community",
    draft = blankDraft(),
    saved = null,
    sending = false,
    submission = 0,
    searchTimer;

  function time(value) {
    const element = n("time", new Date(value).toLocaleString(I18n.language, {
      dateStyle: "medium",
      timeStyle: "short",
    }));
    element.dateTime = value;
    return element;
  }

  function badges(item) {
    const group = n("div", undefined, "idea-tags");
    const category = n("span", tr(categories[item.category] || "Другое"), "idea-category");
    category.dataset.category = item.category;
    const status = n("span", tr(statuses[item.status] || statuses.pending), "idea-status");
    status.dataset.status = item.status;
    group.append(category, status);
    return group;
  }

  function metadata(item, showPublicationDate = false) {
    const meta = n("div", undefined, "idea-meta");
    const publishedAt = showPublicationDate && item.status === "approved" && item.published_at;
    const date = time(publishedAt || item.created_at);
    date.title = tr(publishedAt ? "Дата публикации" : "Дата создания");
    if (showPublicationDate && !publishedAt) date.prepend(tr("Создано") + " ");
    meta.append(n("span", item.author), date);
    return meta;
  }

  function makeForm(host) {
    const form = n("form", undefined, "idea-form");
    host.append(form);
    form.append(n("p", tr("После отправки идея появится в каталоге со статусом «На рассмотрении»."), "muted"));
    const title = UI.field(form, "Название идеи", "idea-title", draft.title);
    title.required = true;
    title.autocomplete = "off";
    UI.choice(form, "Категория", Object.entries(categories), draft.category, (value) => {
      draft.category = value;
      draft.request = requestKey();
    });
    const label = n("label"),
      text = n("textarea"),
      counter = n("small", undefined, "idea-counter");
    text.id = "idea-description";
    text.rows = 8;
    text.required = true;
    text.value = draft.description;
    text.setAttribute("aria-describedby", "idea-counter");
    counter.id = "idea-counter";
    label.append(n("span", tr("Описание идеи")), text, counter);
    form.append(label);

    const feedback = n("div", undefined, "idea-feedback");
    feedback.setAttribute("role", "status");
    feedback.hidden = saved === null;
    if (saved !== null) {
      feedback.append(n("strong", tr("Идея отправлена")));
      feedback.append(b("Открыть мою идею", () => openSaved()));
    }

    const submit = n("button", tr("Отправить идею"), "terminal-action");
    submit.type = "submit";
    form.append(
      n("p", tr("До 10 предложений в час. Название — до 80 символов, описание — до 3000."), "muted"),
      submit,
      feedback,
    );

    function validate() {
      const t = count(title.value),
        d = count(text.value),
        validTitle = t >= 3 && t <= 80,
        validText = d >= 10 && d <= 3000;
      title.setCustomValidity(validTitle ? "" : tr("Название идеи: от 3 до 80 символов"));
      text.setCustomValidity(validText ? "" : tr("Описание идеи: от 10 до 3000 символов"));
      counter.textContent = d + " / 3000";
      counter.classList.toggle("over-limit", d > 3000);
      submit.disabled = sending || !validTitle || !validText;
    }
    function enableFields() {
      form.querySelectorAll("input,textarea,button").forEach((element) => {
        element.disabled = sending;
      });
      form.setAttribute("aria-busy", String(sending));
      validate();
    }
    title.oninput = () => {
      draft.title = title.value;
      draft.request = requestKey();
      validate();
    };
    text.oninput = () => {
      draft.description = text.value;
      draft.request = requestKey();
      validate();
    };
    form.onsubmit = async (event) => {
      event.preventDefault();
      if (sending || !form.reportValidity()) return;
      const serial = ++submission,
        epoch = Session.epoch;
      const current = () => serial === submission && epoch === Session.epoch && Session.signedIn;
      let accepted = false;
      sending = true;
      enableFields();
      try {
        const result = await api("ideas", { ...draft });
        if (!current()) return;
        accepted = true;
        saved = result.id;
        draft = blankDraft();
        views.mine.loaded = 0;
        if (active("mine")) load("mine");
      } catch (error) {
        if (current()) notify(error.message, "error");
      } finally {
        if (current()) {
          sending = false;
          if (active("form")) {
            // The form can be reopened while the POST is pending.
            if (accepted || !form.isConnected) renderPlayer();
            else enableFields();
            if (accepted) player.querySelector(".idea-feedback button")?.focus();
          }
        }
      }
    };
    enableFields();
  }

  function openSaved() {
    const state = views.mine;
    Object.assign(state, initialView(), { selected: saved, mobileDetail: true });
    changeTab("mine");
  }

  function addSelect(host, label, choices, value, change) {
    const group = n("label"),
      select = n("select");
    group.append(n("span", tr(label)), select);
    for (const [id, text] of choices) {
      const option = n("option", tr(text));
      option.value = id;
      select.append(option);
    }
    select.value = value;
    select.onchange = () => change(select.value);
    host.append(group);
  }

  function changeFilters(scope, values, delay = 0) {
    const state = views[scope];
    clearTimeout(searchTimer);
    state.requests.invalidate();
    Object.assign(state, values, {
      page: 1, selected: null, scroll: 0, mobileDetail: false,
      data: null, error: "", loading: true,
    });
    renderHistory(scope);
    if (delay) searchTimer = setTimeout(() => load(scope), delay);
    else load(scope);
  }

  function makeCatalog(host, scope) {
    const state = views[scope],
      grid = n("div", undefined, scope === "all" ? "ideas-admin-layout" : "ideas-catalog"),
      list = n("section", undefined, "ideas-history"),
      summary = n("p", undefined, "ideas-summary"),
      entries = n("div", undefined, "idea-entries"),
      pagination = n("div", undefined, "ideas-pages"),
      detail = n("article", undefined, "idea-detail");
    summary.setAttribute("role", "status");
    entries.onscroll = () => {
      if (entries.clientHeight) state.scroll = entries.scrollTop;
    };
    list.append(summary, entries, pagination);
    grid.append(list, detail);
    if (scope !== "all") {
      const filters = n("div", undefined, "ideas-filters");
      const search = UI.field(filters, "Поиск по названию и описанию", "ideas-search", state.search, "search");
      search.oninput = () => {
        // Count code points without cutting an emoji in half.
        const query = [...search.value].slice(0, 120).join("");
        if (query !== search.value) search.value = query;
        changeFilters(scope, { search: query }, 250);
      };
      addSelect(filters, "Категория", [["", "Все категории"], ...Object.entries(categories)],
        state.category, (category) => changeFilters(scope, { category }));
      addSelect(filters, "Сортировка", [["newest", "Сначала новые"], ["oldest", "Сначала старые"]],
        state.sort, (sort) => changeFilters(scope, { sort }));
      host.append(filters);
    }
    host.append(grid);
    panels[scope] = { grid, summary, entries, pagination, detail };
    renderHistory(scope);
  }

  function renderDetail(scope, item) {
    const state = views[scope],
      { grid, entries, detail } = panels[scope];
    grid.dataset.detailOpen = String(Boolean(item && state.mobileDetail));
    if (!item) {
      detail.replaceChildren();
      if (state.data?.items.length) UI.empty(detail, "Выберите идею из списка");
      return;
    }
    const scroll = detail.dataset.idea === String(item.id) ? detail.scrollTop : 0;
    detail.replaceChildren();
    detail.dataset.idea = String(item.id);
    if (scope !== "all") {
      const back = b("К списку идей", () => {
        state.mobileDetail = false;
        grid.dataset.detailOpen = "false";
        entries.scrollTop = state.scroll;
        entries.querySelector('[aria-pressed="true"]')?.focus({ preventScroll: true });
      });
      back.classList.add("ideas-back");
      detail.append(back);
    }
    const title = n("h2", item.title);
    title.tabIndex = -1;
    detail.append(
      badges(item),
      title,
      metadata(item),
      n("p", item.description, "idea-description"),
    );
    if (scope === "all" && admin()) {
      const controls = n("div", undefined, "inline-controls idea-review");
      let status = item.status;
      UI.choice(controls, "Статус идеи", Object.entries(statuses), status, (value) => {
        status = value;
      });
      controls.append(b("Сохранить статус", async () => {
        const epoch = Session.epoch;
        if (!admin()) return;
        await api("admin/ideas", { id: item.id, status });
        if (epoch !== Session.epoch || !admin()) return;
        for (const scope of ["all", "community", "mine"]) {
          views[scope].loaded = 0;
          if (active(scope)) load(scope);
        }
      }));
      detail.append(controls);
    }
    detail.scrollTop = scroll;
  }

  function selectIdea(scope, item, row) {
    const state = views[scope],
      { entries, detail } = panels[scope];
    state.selected = item.id;
    state.scroll = entries.scrollTop;
    state.mobileDetail = true;
    entries.querySelector('[aria-pressed="true"]')?.setAttribute("aria-pressed", "false");
    row.setAttribute("aria-pressed", "true");
    renderDetail(scope, item);
    if (detail.querySelector(".ideas-back")?.getClientRects().length)
      detail.querySelector("h2")?.focus({ preventScroll: true });
  }

  function renderHistory(scope) {
    const panel = panels[scope];
    if (!panel) return;
    const state = views[scope],
      { grid, summary, entries, pagination } = panel,
      data = state.data;
    const focused = entries.contains(document.activeElement)
      ? document.activeElement.dataset.idea
      : null;
    summary.textContent = data ? tr("Идей: {total}", { total: data.total }) : "";
    entries.replaceChildren();
    pagination.replaceChildren();
    grid.classList.toggle("ideas-no-results", !data?.items.length);
    if (state.error) {
      UI.empty(entries, state.error);
      entries.append(b("Повторить загрузку", () => load(scope)));
    } else if (!data) {
      UI.empty(entries, "Загрузка идей…");
    } else if (!data.items.length) {
      UI.empty(entries, state.search.trim() || state.category
        ? "По вашему запросу ничего не найдено"
        : scope === "mine"
          ? "У вас пока нет предложений"
          : scope === "community"
            ? "Идей сообщества пока нет"
            : "Предложений пока нет");
    } else {
      const selected = data.items.find((item) => item.id === state.selected);
      if (!selected) {
        state.selected = data.items[0].id;
        state.mobileDetail = false;
      }
      for (const item of data.items) {
        const row = n("button", undefined, "idea-list-row");
        row.type = "button";
        row.dataset.idea = item.id;
        row.setAttribute("aria-pressed", String(state.selected === item.id));
        row.append(
          n("strong", item.title),
          badges(item),
          metadata(item, scope === "community"),
        );
        if (scope !== "all") {
          const text = [...item.description.replace(/\s+/gu, " ")];
          row.append(n("span", text.slice(0, 160).join("") + (text.length > 160 ? "…" : ""), "idea-excerpt"));
        }
        row.onclick = () => selectIdea(scope, item, row);
        entries.append(row);
        if (String(item.id) === focused) row.focus({ preventScroll: true });
      }
      if (data.pages > 1) {
        const pages = pager({
          page: data.page, pages: data.pages,
          change(page) {
            Object.assign(state, {
              page, scroll: 0, selected: null, mobileDetail: false, data: null,
            });
            return load(scope);
          },
        });
        pages.firstElementChild.setAttribute("aria-label", tr("Предыдущая страница"));
        pages.lastElementChild.setAttribute("aria-label", tr("Следующая страница"));
        pagination.append(pages);
      }
    }
    renderDetail(scope, data?.items.find((item) => item.id === state.selected));
    if (entries.clientHeight) entries.scrollTop = state.scroll;
  }

  async function load(scope) {
    if (!active(scope)) return;
    const state = views[scope],
      task = state.requests.begin(),
      query = new URLSearchParams({ scope, page: state.page });
    if (scope !== "all") {
      query.set("sort", state.sort);
      if (state.search.trim()) query.set("search", state.search.trim());
      if (state.category) query.set("category", state.category);
    }
    state.loading = true;
    state.error = "";
    if (!state.data) renderHistory(scope);
    try {
      const data = await api("ideas?" + query, undefined, task.signal);
      if (!task.current()) return;
      const changed = JSON.stringify(data) !== JSON.stringify(state.data);
      state.data = data;
      state.page = data.page;
      state.loaded = Date.now();
      if (changed) renderHistory(scope);
    } catch (error) {
      if (!task.current()) return;
      state.data = null;
      state.error = error.kind === "network" ? "Нет соединения с сервером" : "Не удалось загрузить идеи";
      renderHistory(scope);
    } finally {
      if (task.current()) state.loading = false;
    }
  }

  function stop(scope) {
    views[scope].requests.invalidate();
    views[scope].loading = false;
    delete panels[scope];
  }

  function renderPlayer() {
    player.replaceChildren();
    const host = n("div", undefined, "ideas-panel");
    const tabbar = UI.tabs(player, tabs, tab, changeTab, { panel: host });
    tabbar.setAttribute("aria-label", tr("Разделы предложений"));
    player.append(host);
    if (tab === "form") makeForm(host);
    else makeCatalog(host, tab);
  }

  function changeTab(next) {
    if (next === tab) return;
    if (tab !== "form") {
      clearTimeout(searchTimer);
      stop(tab);
    }
    tab = next;
    renderPlayer();
    player.querySelector('[aria-selected="true"]')?.focus({ preventScroll: true });
    if (tab !== "form") load(tab);
  }

  window.addEventListener("moduleopen", ({ detail: id }) => {
    if (!Session.signedIn) return;
    if (id === "ideas") {
      renderPlayer();
      if (tab !== "form") load(tab);
    }
    if (id === "adminideas") {
      if (!admin()) {
        Modules.close(id);
        return;
      }
      review.replaceChildren();
      makeCatalog(review, "all");
      load("all");
    }
  });
  window.addEventListener("gamedata", () => {
    if (!admin() && (Modules.isOpen("adminideas") || views.all.data)) {
      Modules.close("adminideas");
      Object.assign(views.all, initialView());
      review.replaceChildren();
    }
    for (const [scope, state] of Object.entries(views))
      if (active(scope) && !state.loading && !state.error && Date.now() - state.loaded > 15000)
        load(scope);
  });
  window.addEventListener("moduleclose", ({ detail: id }) => {
    if (id === "ideas" && tab !== "form") {
      clearTimeout(searchTimer);
      stop(tab);
    }
    if (id === "adminideas") stop("all");
  });
  window.addEventListener("languagechange", () => {
    if (Modules.isOpen("ideas")) renderPlayer();
    if (active("all")) {
      review.replaceChildren();
      makeCatalog(review, "all");
    }
  });
  window.addEventListener("signedout", () => {
    clearTimeout(searchTimer);
    submission++;
    sending = false;
    saved = null;
    draft = blankDraft();
    tab = "community";
    for (const scope of Object.keys(views)) {
      stop(scope);
      Object.assign(views[scope], initialView());
    }
    player.replaceChildren();
    review.replaceChildren();
    Modules.close("ideas");
    Modules.close("adminideas");
  });
}
