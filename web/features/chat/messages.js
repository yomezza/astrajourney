import { node as n } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";
const tr = (s) => I18n.t(s);
export function renderMessages(rows, context) {
  const result = [];
  const input = context.tab === "favorites" ? rows : [...rows].reverse();
  for (const m of input) {
    if (context.tab === "favorites") {
      const card = n("article", undefined, "chat-favorite-card"),
        head = n("div", undefined, "chat-favorite-card-head"),
        meta = n("span"),
        more = n("button", "⋯", "chat-small-btn");
      card.dataset.id = m.id;
      meta.append(
        n(
          "span",
          tr(m.kind === "note" ? "Заметка" : "Копия сообщения"),
          "chat-folder-label",
        ),
        document.createTextNode(
          " · " + new Date(m.saved_at).toLocaleString(I18n.language),
        ),
      );
      more.type = "button";
      more.setAttribute("aria-label", tr("Действия с сообщением"));
      more.onclick = (e) => context.menu(m, e);
      card.oncontextmenu = (e) => context.menu(m, e);
      head.append(meta, more);
      card.append(
        head,
        n("p", m.body),
        n(
          "span",
          m.kind === "note"
            ? tr("Личная заметка")
            : m.author_name_snapshot || m.author || "",
          "chat-mini",
        ),
      );
      result.push(card);
      continue;
    }

    if (m.system_code) {
      result.push(
        n(
          "div",
          tr(
            {
              joined: "Пилот присоединился к беседе",
              left: "Пилот покинул беседу",
              kicked: "Пилот исключён из беседы",
              renamed: "Название беседы изменено",
              owner_changed: "Владелец беседы изменён",
            }[m.system_code] || "Событие беседы",
          ),
          "chat-system-message",
        ),
      );
      continue;
    }
    const row = n("article", undefined, "chat-message");
    row.dataset.id = m.id;
    row.dataset.author = m.author_id || "";
    if (m.seq) row.dataset.seq = m.seq;
    if (String(m.author_id) === String(context.user)) row.classList.add("own");
    const main = n("div", undefined, "chat-msg-content"),
      head = n("div", undefined, "chat-msg-head"),
      author = n(
        "button",
        m.author || m.author_name_snapshot || tr("Личная заметка"),
        "chat-author",
      ),
      at = n(
        "time",
        new Date(m.sent_at || m.saved_at).toLocaleTimeString(I18n.language, {
          hour: "2-digit",
          minute: "2-digit",
        }),
        "chat-time",
      );
    at.dateTime = m.sent_at || m.saved_at;
    author.onclick = () =>
      m.author_id &&
      context.mention({
        id: m.author_id,
        name: m.author || m.author_name_snapshot,
      });
    head.append(at, author);
    if (m.tag) head.append(n("span", "[" + m.tag + "]", "chat-corp"));
    if (context.tab === "private" && row.classList.contains("own")) {
      const receipt = n(
        "span",
        undefined,
        "chat-read-dot" + (m.read ? " read" : ""),
      );
      receipt.tabIndex = 0;
      receipt.dataset.help = tr(
        m.read ? "Прочитано хотя бы одним участником" : "Ещё не прочитано",
      );
      head.append(receipt);
    }
    const body = n("p", undefined, "chat-msg-text"),
      chars = Array.from(m.body || "");
    let cursor = 0;
    for (const entity of m.entities || []) {
      if (entity.start < cursor || entity.end > chars.length) continue;
      body.append(
        document.createTextNode(chars.slice(cursor, entity.start).join("")),
      );
      const label = chars.slice(entity.start, entity.end).join(""),
        link = n("button", label, "chat-inline-link");
      link.onclick = () => {
        const v =
          entity.kind === "mention"
            ? context.mention({ id: entity.id, name: entity.label })
            : context.openLink(entity);
        Promise.resolve(v).catch(() => {});
      };
      body.append(link);
      cursor = entity.end;
    }
    body.append(document.createTextNode(chars.slice(cursor).join("")));
    main.append(head);
    if (m.reply_to)
      main.append(
        n(
          "div",
          m.reply_body
            ? m.reply_author + ": " + m.reply_body
            : tr("Исходное сообщение недоступно"),
          "chat-reply-origin",
        ),
      );
    main.append(body);
    const more = n("button", "⋯", "chat-more");
    more.setAttribute("aria-label", tr("Действия с сообщением"));
    more.onclick = (e) => context.menu(m, e);
    row.oncontextmenu = (e) => context.menu(m, e);
    row.append(main, more);
    if (m.sector != null)
      row.prepend(n("span", String(m.sector), "chat-sector-no"));
    result.push(row);
  }
  if (!rows.length) result.push(n("p", tr("Пока нет сообщений"), "chat-empty"));
  return result;
}
const emojiGroups = [
  [
    "Эмоции",
    [
      ["😀", "улыбка smile"],
      ["😁", "радость happy"],
      ["😂", "смех laugh"],
      ["🙂", "улыбка smile"],
      ["😉", "подмигивание wink"],
      ["🤔", "мысль thinking"],
      ["😢", "грусть sad"],
      ["😡", "злость angry"],
      ["😴", "сон sleep"],
      ["😎", "круто cool"],
      ["🥳", "праздник party"],
      ["❤️", "сердце heart"],
    ],
  ],
  [
    "Жесты",
    [
      ["👍", "хорошо thumb"],
      ["👎", "плохо thumb"],
      ["👋", "привет wave"],
      ["👏", "аплодисменты clap"],
      ["🤝", "согласие handshake"],
      ["🙏", "спасибо thanks"],
      ["💪", "сила strong"],
    ],
  ],
  [
    "Космос",
    [
      ["🚀", "ракета rocket"],
      ["🛰️", "спутник satellite"],
      ["🌍", "планета planet"],
      ["🌙", "луна moon"],
      ["⭐", "звезда star"],
      ["☄️", "комета comet"],
      ["🪐", "сатурн saturn"],
      ["🌌", "галактика galaxy"],
      ["🧑‍🚀", "космонавт astronaut"],
      ["🔭", "телескоп telescope"],
    ],
  ],
  [
    "Объекты",
    [
      ["⛏️", "добыча mining"],
      ["🔧", "ремонт repair"],
      ["⚙️", "механизм gear"],
      ["💎", "кристалл crystal"],
      ["📦", "груз cargo"],
      ["⚡", "энергия energy"],
      ["✅", "готово check"],
      ["⚠️", "внимание warning"],
      ["❌", "нет cancel"],
    ],
  ],
];
const carets = new WeakMap();
export function emojiPicker(compose, textarea) {
  let panel = compose.querySelector(".chat-emoji-panel");
  if (panel) {
    panel.hidden = !panel.hidden;
    return;
  }
  panel = n("section", undefined, "chat-emoji-panel");
  panel.setAttribute("aria-label", tr("Эмодзи"));
  const head = n("div", undefined, "chat-emoji-head"),
    close = n("button", "×");
  close.type = "button";
  close.onclick = () => {
    panel.hidden = true;
    textarea.focus();
  };
  head.append(n("strong", tr("Эмодзи")), close);
  const search = n("input");
  search.type = "search";
  search.placeholder = tr("Поиск эмодзи");
  search.setAttribute("aria-label", search.placeholder);
  search.className = "chat-emoji-search";
  const categories = n("div", undefined, "chat-emoji-categories"),
    grid = n("div", undefined, "chat-emoji-grid");
  let current = 0,
    state = carets.get(textarea);
  if (!state) {
    state = { caret: textarea.selectionStart, end: textarea.selectionEnd };
    carets.set(textarea, state);
    const update = () => {
      state.caret = textarea.selectionStart;
      state.end = textarea.selectionEnd;
    };
    textarea.addEventListener("select", update);
    textarea.addEventListener("keyup", update);
  }
  state.caret = textarea.selectionStart;
  state.end = textarea.selectionEnd;
  function render() {
    grid.replaceChildren();
    for (const [emoji, label] of (search.value
      ? emojiGroups.flatMap((v) => v[1])
      : emojiGroups[current][1]
    ).filter(
      (v) =>
        !search.value ||
        v[1].toLowerCase().includes(search.value.toLowerCase()),
    )) {
      const b = n("button", emoji);
      b.type = "button";
      b.setAttribute("aria-label", label);
      b.onclick = () => {
        textarea.setRangeText(emoji, state.caret, state.end, "end");
        state.caret = state.end = textarea.selectionEnd;
        textarea.dispatchEvent(new Event("input"));
      };
      grid.append(b);
    }
  }
  emojiGroups.forEach(([label], i) => {
    const b = n("button", tr(label), "chat-emoji-category");
    b.type = "button";
    b.onclick = () => {
      current = i;
      search.value = "";
      render();
    };
    categories.append(b);
  });
  search.oninput = render;
  panel.onkeydown = (e) => {
    if (e.key === "Escape") {
      panel.hidden = true;
      textarea.focus();
      e.stopPropagation();
    }
  };
  panel.append(head, search, categories, grid);
  compose.append(panel);
  render();
}
