import { pager } from "../../ui/pager.js";

import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";

export function initIdeas() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button;
  const roots = {
    ideas: Modules.create("ideas", "Предложить идею", 1000, 720),
    adminideas: Modules.create("adminideas", "Предложения игроков", 1060, 720, {
      audience: "admin",
    }),
  };
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
  function statusTag(status = "pending") {
    const tag = n(
      "span",
      tr(statuses[status] || statuses.pending),
      "idea-status",
    );
    tag.dataset.status = status;
    return tag;
  }
  const uuid = () =>
    [...crypto.getRandomValues(new Uint32Array(4))]
      .map((v) => v.toString(16).padStart(8, "0"))
      .join("");
  const blank = () => ({
    title: "",
    description: "",
    category: "gameplay",
    request: uuid(),
  });
  let draft = blank(),
    sending = false,
    submission = 0,
    saved = null;
  const views = {
    ideas: { page: 1, request: 0, loading: false, loaded: 0 },
    adminideas: {
      page: 1,
      request: 0,
      loading: false,
      loaded: 0,
      selected: null,
    },
  };
  const admin = () =>
    Session.signedIn && Session.data.state?.user.role === "admin";
  const count = (s) => Array.from(s.trim()).length;
  const date = (v) =>
    new Date(v).toLocaleString(I18n.language, {
      dateStyle: "medium",
      timeStyle: "short",
    });
  function tag(category) {
    const e = n("span", tr(categories[category] || "Другое"), "idea-category");
    e.dataset.category = category;
    return e;
  }
  function header(root, title, text) {
    const h = n("div", undefined, "ideas-heading"),
      icon = n("span", "✧", "ideas-mark"),
      body = n("div");
    body.append(n("h3", tr(title)), n("p", tr(text), "muted"));
    h.append(icon, body);
    root.append(h);
  }
  function renderForm() {
    const root = roots.ideas;
    root.classList.add("ideas-content");
    root.replaceChildren();
    header(
      root,
      "Есть идея для Эдема?",
      "Расскажите, что стоит добавить или улучшить. Предложение попадёт на рассмотрение администрации.",
    );
    const grid = n("div", undefined, "ideas-layout"),
      form = n("form", undefined, "idea-form"),
      history = n("section", undefined, "ideas-history");
    grid.append(form, history);
    root.append(grid);
    const title = UI.field(form, "Название идеи", "idea-title", draft.title);
    title.required = true;
    title.autocomplete = "off";
    const label = n("label"),
      caption = n("span", tr("Описание идеи")),
      text = n("textarea"),
      counter = n("small", undefined, "idea-counter");
    text.id = "idea-description";
    text.rows = 9;
    text.required = true;
    text.value = draft.description;
    text.placeholder = tr(
      "Что вы предлагаете, как это должно работать и чем поможет игрокам?",
    );
    label.append(caption, text, counter);
    UI.choice(
      form,
      "Категория",
      Object.entries(categories),
      draft.category,
      (value) => {
        draft.category = value;
        draft.request = uuid();
      },
    );
    form.append(label);
    const note = n(
      "p",
      tr(
        "До 10 предложений в час. Название — до 80 символов, описание — до 3000.",
      ),
      "muted",
    );
    const feedback = n("div", undefined, "idea-feedback");
    feedback.setAttribute("role", "status");
    if (saved)
      feedback.append(
        n("strong", tr("Идея отправлена") + " · №" + saved),
        n(
          "p",
          tr("Предложение сохранено. Спасибо за участие в развитии игры."),
        ),
      );
    feedback.hidden = !saved;
    async function send() {
      if (sending || !form.reportValidity()) return;
      const serial = ++submission,
        epoch = Session.epoch,
        payload = { ...draft };
      sending = true;
      form
        .querySelectorAll("input,textarea,button")
        .forEach((e) => (e.disabled = true));
      try {
        const result = await api("ideas", payload);
        if (
          serial !== submission ||
          epoch !== Session.epoch ||
          !Session.signedIn
        )
          return;
        saved = result.id;
        draft = blank();
        views.ideas.page = 1;
        await load("ideas");
      } catch (e) {
        if (
          serial === submission &&
          epoch === Session.epoch &&
          Session.signedIn
        )
          notify(e.message, "error");
      } finally {
        if (serial === submission) {
          sending = false;
          form
            .querySelectorAll("input,textarea,button")
            .forEach((e) => (e.disabled = false));
          if (Modules.isOpen("ideas")) renderForm();
        }
      }
    }
    const submit = b("Отправить идею", (e) => {
      e.preventDefault();
      return send();
    });
    submit.type = "submit";
    form.onsubmit = (e) => {
      e.preventDefault();
      send();
    };
    function validate() {
      const t = count(title.value),
        d = count(text.value);
      title.setCustomValidity(
        t < 3 || t > 80 ? tr("Название идеи: от 3 до 80 символов") : "",
      );
      text.setCustomValidity(
        d < 10 || d > 3000 ? tr("Описание идеи: от 10 до 3000 символов") : "",
      );
      counter.textContent = d + " / 3000";
      counter.classList.toggle("over-limit", d > 3000);
      submit.disabled = sending || t < 3 || t > 80 || d < 10 || d > 3000;
    }
    title.oninput = () => {
      draft.title = title.value;
      draft.request = uuid();
      saved = null;
      feedback.hidden = true;
      validate();
    };
    text.oninput = () => {
      draft.description = text.value;
      draft.request = uuid();
      saved = null;
      feedback.hidden = true;
      validate();
    };
    form.append(note, submit, feedback);
    validate();
    if (sending)
      form
        .querySelectorAll("input,textarea,button")
        .forEach((e) => (e.disabled = true));
    if (views.ideas.data) renderHistory("ideas");
  }
  function renderAdmin() {
    const root = roots.adminideas;
    root.classList.add("ideas-content");
    root.replaceChildren();
    header(
      root,
      "Предложения игроков",
      "Идеи пилотов с автором, категорией и датой отправки.",
    );
    const grid = n("div", undefined, "ideas-admin-layout"),
      list = n("section", undefined, "ideas-history"),
      detail = n("article", undefined, "idea-detail");
    grid.append(list, detail);
    root.append(grid);
    if (views.adminideas.data) renderHistory("adminideas");
  }
  function renderDetail(item) {
    const root = roots.adminideas.querySelector(".idea-detail");
    if (!root) return;
    const scroll = root.dataset.idea === String(item?.id) ? root.scrollTop : 0;
    root.replaceChildren();
    root.dataset.idea = String(item?.id || "");
    if (!item) {
      UI.empty(root, "Выберите предложение из списка");
      return;
    }
    root.append(
      tag(item.category),
      statusTag(item.status),
      n("h2", item.title),
      n("p", item.author + " · " + date(item.created_at), "idea-meta"),
      n("p", item.description, "idea-description"),
    );
    if (admin()) {
      const controls = n("div", undefined, "inline-controls idea-review");
      let status = item.status || "pending";
      UI.choice(
        controls,
        "Статус идеи",
        Object.entries(statuses),
        status,
        (value) => {
          status = value;
        },
      );
      controls.append(
        b("Сохранить статус", async () => {
          const epoch = Session.epoch;
          if (!admin()) return;
          await api("admin/ideas", { id: item.id, status });
          if (epoch !== Session.epoch || !admin()) return;
          await load("adminideas");
          if (Modules.isOpen("ideas")) await load("ideas");
        }),
      );
      root.append(controls);
    }
    root.scrollTop = scroll;
  }
  function renderHistory(id) {
    const state = views[id],
      host = roots[id].querySelector(".ideas-history"),
      data = state.data;
    if (!host || !data) return;
    const opened = new Set(
      [...host.querySelectorAll("details[open]")].map((e) => e.dataset.idea),
    );
    host.replaceChildren(
      n(
        "h3",
        tr(id === "ideas" ? "Мои предложения" : "Все предложения") +
          " · " +
          data.total,
      ),
    );
    const entries = n("div", undefined, "idea-entries");
    host.append(entries);
    if (id === "adminideas" && !data.items.some((i) => i.id === state.selected))
      state.selected = data.items[0]?.id;
    for (const item of data.items) {
      if (id === "adminideas") {
        const card = b("", () => {
          state.selected = item.id;
          renderHistory(id);
        });
        card.className = "idea-list-row";
        card.classList.toggle("selected", state.selected === item.id);
        card.append(
          tag(item.category),
          statusTag(item.status),
          n("b", item.title),
          n("small", item.author + " · " + date(item.created_at)),
        );
        entries.append(card);
      } else {
        const details = n("details", undefined, "idea-sent-card"),
          summary = n("summary");
        details.dataset.idea = String(item.id);
        details.open = opened.has(String(item.id));
        summary.append(
          n("b", item.title),
          tag(item.category),
          statusTag(item.status),
          n("small", date(item.created_at)),
        );
        details.append(summary, n("p", item.description, "idea-description"));
        entries.append(details);
      }
    }
    if (!data.items.length)
      UI.empty(
        entries,
        id === "ideas" ? "Пока нет отправленных идей" : "Предложений пока нет",
      );
    if (data.pages > 1) {
      const pages = pager({
        page: data.page,
        pages: data.pages,
        change: (value) => {
          state.page = value;
          return load(id);
        },
      });
      host.append(pages);
    }
    if (id === "adminideas")
      renderDetail(data.items.find((i) => i.id === state.selected));
  }
  async function load(id) {
    const state = views[id],
      request = ++state.request,
      epoch = Session.epoch;
    state.loading = true;
    try {
      const data = await api(
        "ideas?scope=" +
          (id === "adminideas" ? "all" : "mine") +
          "&page=" +
          state.page,
      );
      if (
        request !== state.request ||
        epoch !== Session.epoch ||
        !Session.signedIn ||
        !Modules.isOpen(id)
      )
        return;
      state.data = data;
      state.page = data.page;
      state.loaded = Date.now();
      renderHistory(id);
    } catch (e) {
      if (
        epoch === Session.epoch &&
        Session.signedIn &&
        request === state.request
      )
        notify(e.message, "error");
    } finally {
      if (request === state.request) state.loading = false;
    }
  }
  window.addEventListener("moduleopen", (e) => {
    if (!(e.detail in roots) || !Session.signedIn) return;
    if (e.detail === "adminideas" && !admin()) {
      Modules.close(e.detail);
      return;
    }
    if (e.detail === "ideas") renderForm();
    else renderAdmin();
    Modules.wait(e.detail, load(e.detail));
  });
  window.addEventListener("languagechange", () => {
    if (Modules.isOpen("ideas")) renderForm();
    if (Modules.isOpen("adminideas") && admin()) renderAdmin();
  });
  window.addEventListener("gamedata", () => {
    if (!admin() && Modules.isOpen("adminideas")) Modules.close("adminideas");
    for (const id of Object.keys(roots))
      if (
        Modules.isOpen(id) &&
        !views[id].loading &&
        Date.now() - views[id].loaded > 15000
      )
        load(id);
  });
  window.addEventListener("moduleclose", (e) => {
    if (views[e.detail]) {
      views[e.detail].request++;
      views[e.detail].loading = false;
    }
  });
  window.addEventListener("signedout", () => {
    submission++;
    sending = false;
    saved = null;
    draft = blank();
    for (const id of Object.keys(roots)) {
      views[id].request++;
      views[id].loading = false;
      views[id].data = null;
      views[id].page = 1;
      views[id].loaded = 0;
      views[id].selected = null;
      roots[id].replaceChildren();
      Modules.close(id);
    }
  });
}
