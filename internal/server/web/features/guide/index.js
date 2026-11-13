import { Session, RequestScope } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { api } from "../../core/api.js";
import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";
import { gameCards, renderGameArticle } from "./game-catalog.js";
import { renderBlocks } from "./blocks.js";

const categories = {
  "game:resources": "Ресурсы",
  "game:ships": "Корабли",
  "game:objects": "Космические объекты",
  "game:technical": "Техническая информация",
};
export function createGuide({ roots, shipStats, filterShipyards }) {
  const root = roots.guide,
    n = UI.node,
    b = UI.button,
    tr = (s) => I18n.t(s),
    requests = new RequestScope();
  let owner = null,
    nodes = null,
    query = "",
    tab = "all",
    category = "",
    selected = "game:resource:iron",
    favorites = new Set(),
    recent = [],
    cms = [],
    categoryRows = [],
    assets = {},
    loading = false,
    loadedAt = 0,
    revision = 0,
    articleToken = 0,
    indexKey = "",
    readerKey = "",
    searchHits = null,
    searchTimer = null;
  const searching = new RequestScope();
  const preference = () =>
    Preferences.key("guide", { account: owner, version: 1 });
  const save = () =>
    Preferences.write(preference(), {
      favorites: [...favorites].slice(0, 500),
      recent: recent.slice(0, 50),
    });
  const all = () => [
    ...gameCards().map((v) => ({
      ...v,
      title: tr(v.name),
      summary: tr(v.desc || ""),
      source: "game",
      category_id: v.category,
    })),
    ...cms,
  ];
  const categoryTitle = (id) => {
    if (categories[id]) return tr(categories[id]);
    const row = categoryRows.find((c) => c.id === id);
    if (!row) return tr("База знаний");
    return row.title + (row.fallback ? " · RU" : "");
  };
  function account() {
    const id = Session.data.state?.user.id;
    if (id !== owner) {
      clear();
      owner = id;
      const stored = Preferences.read(
        preference(),
        {},
        (v) => v && typeof v === "object",
      );
      favorites = new Set(
        Array.isArray(stored.favorites)
          ? stored.favorites.filter((v) => typeof v === "string").slice(0, 500)
          : [],
      );
      recent = Array.isArray(stored.recent)
        ? stored.recent.filter((v) => typeof v === "string").slice(0, 50)
        : [];
    }
  }
  function layout() {
    if (nodes && root.contains(nodes.body)) return;
    const body = n("div", undefined, "mode-c"),
      hero = n("div", undefined, "c-hero"),
      top = n("div", undefined, "c-hero-top"),
      count = n("small", undefined, "tiny-caption"),
      searchWrap = n("div", undefined, "c-big-search"),
      search = n("input"),
      tabs = n("div", undefined, "c-tabs"),
      main = n("div", undefined, "c-main"),
      index = n("aside", undefined, "c-index guide-scroll"),
      reading = n("div", undefined, "c-reading guide-scroll"),
      chips = n("div", undefined, "c-category-chips"),
      list = n("div");
    search.type = "search";
    search.value = query;
    search.placeholder = tr("Что вы хотите узнать?");
    search.setAttribute("aria-label", tr("Поиск по базе знаний"));
    search.oninput = () => {
      query = search.value;
      searchHits = null;
      drawList();
      clearTimeout(searchTimer);
      searchTimer = setTimeout(searchCMS, 220);
    };
    top.append(n("strong", tr("База знаний пилота")), count);
    searchWrap.append(n("span", "⌕"), search);
    hero.append(top, searchWrap);
    for (const [value, title] of [
      ["all", "Все темы"],
      ["favorites", "Избранное"],
      ["recent", "Недавние"],
    ]) {
      const button = b(title, () => {
        tab = value;
        drawList();
      });
      button.dataset.tab = value;
      tabs.append(button);
    }
    index.append(chips, list);
    main.append(index, reading);
    body.append(hero, tabs, main);
    root.replaceChildren(body);
    nodes = { body, count, search, tabs, index, reading, chips, list };
    indexKey = readerKey = "";
  }
  function filtered() {
    let list = all();
    const q = query.trim().toLocaleLowerCase(I18n.language);
    if (category) list = list.filter((v) => v.category_id === category);
    if (tab === "favorites") list = list.filter((v) => favorites.has(v.id));
    if (tab === "recent")
      list = recent.map((id) => list.find((v) => v.id === id)).filter(Boolean);
    return list.filter((v) =>
      v.source === "article" && searchHits !== null
        ? searchHits.has(v.id)
        : !q ||
          [
            v.title,
            v.summary,
            v.code,
            categoryTitle(v.category_id),
            v.subcategory,
          ].some((s) =>
            String(s || "")
              .toLocaleLowerCase(I18n.language)
              .includes(q),
          ),
    );
  }
  function drawList() {
    if (!nodes) return;
    const list = filtered(),
      key = JSON.stringify([
        list,
        query,
        tab,
        category,
        [...favorites],
        I18n.language,
      ]);
    nodes.count.textContent = all().length + " · " + tr("тем");
    for (const t of nodes.tabs.children) {
      t.classList.toggle("active", t.dataset.tab === tab);
      t.setAttribute("aria-pressed", String(t.dataset.tab === tab));
    }
    if (indexKey !== key) {
      indexKey = key;
      const scroll = nodes.index.scrollTop;
      nodes.chips.replaceChildren();
      for (const id of ["", ...new Set(all().map((v) => v.category_id))]) {
        const button = b(id ? categoryTitle(id) : "Все темы", () => {
          category = id;
          drawList();
        });
        button.className = "g-chip" + (category === id ? " active" : "");
        nodes.chips.append(button);
      }
      nodes.list.replaceChildren(
        ...list.map((item) => {
          const button = b("", () => openArticle(item.id));
          button.className = "result-row";
          button.dataset.article = item.id;
          const text = n("span");
          text.append(
            n("strong", item.title),
            n("small", item.summary || categoryTitle(item.category_id)),
          );
          button.append(
            n("span", item.source === "game" ? "◆" : "◇", "result-code"),
            text,
            n("span", categoryTitle(item.category_id), "result-meta"),
          );
          return button;
        }),
      );
      if (!list.length) {
        const empty = n("div", undefined, "result-empty");
        empty.append(
          n(
            "b",
            tr(
              tab === "favorites"
                ? "Избранное пусто"
                : tab === "recent"
                  ? "История просмотров пуста"
                  : "Совпадений не найдено",
            ),
          ),
          n("span", tr("Измените запрос или фильтр.")),
        );
        nodes.list.append(empty);
      }
      nodes.index.scrollTop = scroll;
    }
    for (const row of nodes.list.children) {
      row.classList.toggle("active", row.dataset.article === selected);
      row.setAttribute(
        "aria-pressed",
        String(row.dataset.article === selected),
      );
    }
  }
  function canonical(id) {
    id = { belt: "asteroids", ice: "ice-field", volcanic: "lava" }[id] || id;
    if (id.startsWith("game:") || cms.some((v) => v.id === id)) return id;
    const cards = gameCards();
    return (
      cards.find((v) => v.id === "game:resource:" + id)?.id ||
      cards.find((v) => v.id === "game:ship:" + id.replace(/^ship-/, ""))?.id ||
      cards.find((v) => v.id === "game:object:" + id)?.id ||
      (id === "experience" ? "game:technical:experience" : id)
    );
  }
  async function openArticle(id, { remember = true, force = false } = {}) {
    if (!Session.signedIn) return;
    account();
    layout();
    selected = canonical(id);
    const item = all().find((v) => v.id === selected);
    const epoch = Session.epoch;
    if (remember && item) {
      recent = [selected, ...recent.filter((v) => v !== selected)].slice(0, 50);
      save();
    }
    drawList();
    if (!item) {
      nodes.reading.replaceChildren(n("p", tr("Материал недоступен")));
      return;
    }
    const key = JSON.stringify([item, I18n.language, favorites.has(selected)]);
    if (readerKey === key && !force) return;
    readerKey = key;
    const token = ++articleToken;
    const top = nodes.reading.scrollTop;
    const pane = n("article", undefined, "article-pane"),
      line = n("div", undefined, "article-topline"),
      actions = n("div", undefined, "article-actions");
    line.append(
      n("span", categoryTitle(item.category_id), "guide-eyebrow"),
      n(
        "span",
        tr(
          item.source === "game"
            ? "Игровой справочник"
            : "Опубликованная статья",
        ),
        "article-source",
      ),
    );
    const fav = b(
      favorites.has(selected) ? "В избранном" : "В избранное",
      () => {
        favorites.has(item.id)
          ? favorites.delete(item.id)
          : favorites.add(item.id);
        save();
        drawList();
        openArticle(item.id, { remember: false, force: true });
      },
    );
    fav.classList.toggle("saved", favorites.has(selected));
    fav.setAttribute("aria-pressed", String(favorites.has(selected)));
    actions.append(fav);
    pane.append(
      line,
      n("h2", item.title, "guide-title"),
      n("p", item.summary || "", "guide-summary"),
      actions,
    );
    nodes.reading.replaceChildren(pane);
    nodes.reading.scrollTop = top;
    if (item.source === "game")
      renderGameArticle(pane, item, {
        shipStats,
        filterShipyards,
        open: openArticle,
      });
    else
      try {
        const article = await api(
          "guide/articles/" +
            encodeURIComponent(item.id) +
            "?lang=" +
            I18n.language,
        );
        if (
          token !== articleToken ||
          epoch !== Session.epoch ||
          !Session.signedIn ||
          !pane.isConnected
        )
          return;
        if (article.fallback)
          pane.append(
            n(
              "p",
              tr("Перевод отсутствует: показана русская публикация"),
              "guide-fallback",
            ),
          );
        renderBlocks(pane, article.blocks, { open: openArticle, assets });
        const inlineTargets = new Set();
        const visit = (parts) => {
          for (const v of parts || []) {
            if (v.type === "image") inlineTargets.add(v.target);
            visit(v.children);
          }
        };
        for (const block of article.blocks || []) visit(block.content);
        for (const media of article.media || []) {
          if (media.unavailable) {
            pane.append(n("p", tr("Ресурс недоступен"), "muted"));
            continue;
          }
          if (
            inlineTargets.has("builtin:" + media.builtin_asset_key) ||
            inlineTargets.has("media:" + media.uploaded_media_id)
          )
            continue;
          const asset = media.asset;
          if (asset && media.role === "hologram")
            pane.append(
              UI.hologram(
                asset.path.replace(/^\//, ""),
                media.caption || item.title,
                "object-hologram",
              ),
            );
          else {
            const img = n("img");
            img.className = "guide-illustration";
            img.loading = "lazy";
            img.alt =
              media.caption ||
              (I18n.language === "en" ? media.alt_en : media.alt_ru) ||
              "";
            img.src =
              asset?.path || "/api/guide/media/" + media.uploaded_media_id;
            pane.append(img);
          }
        }
        if (article.related?.length) {
          const links = n("div", undefined, "article-links");
          pane.append(n("h3", tr("Связанные материалы")), links);
          for (const v of article.related)
            links.append(b(v.title, () => openArticle(v.id)));
        }
      } catch (error) {
        if (token === articleToken && epoch === Session.epoch) {
          readerKey = "";
          pane.append(
            n("p", error.message, "error"),
            b("Повторить", () =>
              openArticle(item.id, { force: true, remember: false }),
            ),
          );
        }
      }
    if (token !== articleToken || !pane.isConnected) return;
    const list = filtered(),
      at = list.findIndex((v) => v.id === selected),
      previous = list[at - 1],
      next = list[at + 1],
      pagination = n("div", undefined, "reader-pagination");
    pagination.append(
      b("← Предыдущая", () => openArticle(previous.id), !previous),
      n("small", at + 1 + " / " + list.length),
      b("Следующая →", () => openArticle(next.id), !next),
    );
    pane.append(pagination);
    nodes.reading.scrollTop = top;
  }
  async function searchCMS() {
    if (!query.trim() || !Session.signedIn) return;
    const task = searching.begin(),
      q = query,
      lang = I18n.language;
    let cursor = "",
      hits = [];
    try {
      do {
        const data = await api(
          "guide/catalog?lang=" +
            lang +
            "&q=" +
            encodeURIComponent(q) +
            "&cursor=" +
            cursor,
          undefined,
          task.signal,
        );
        if (!task.current() || q !== query || lang !== I18n.language) return;
        hits.push(...data.items.filter((v) => v.source === "article"));
        cursor = data.next_cursor;
      } while (cursor);
      searchHits = new Set(hits.map((v) => v.id));
      for (const item of hits) {
        if (!cms.some((v) => v.id === item.id)) cms.push(item);
      }
      drawList();
    } catch (error) {
      if (task.current() && error.name !== "AbortError")
        notify(error.message, "error");
    }
  }
  async function load() {
    if (loading || !Session.signedIn) return;
    loading = true;
    const task = requests.begin(),
      lang = I18n.language;
    try {
      const manifest = await api("guide/assets", undefined, task.signal);
      let cursor = "",
        items = [],
        rows = [],
        version;
      do {
        const data = await api(
          "guide/catalog?lang=" +
            lang +
            "&cursor=" +
            encodeURIComponent(cursor),
          undefined,
          task.signal,
        );
        if (!task.current() || lang !== I18n.language) return;
        items.push(...data.items.filter((v) => v.source === "article"));
        rows = data.categories;
        version = data.revision;
        cursor = data.next_cursor;
      } while (cursor);
      if (!task.current()) return;
      assets = Object.fromEntries(manifest.items.map((v) => [v.key, v]));
      cms = items;
      categoryRows = rows;
      const changed = revision !== version;
      revision = version;
      loadedAt = Date.now();
      drawList();
      if (changed || !readerKey)
        await openArticle(selected, { remember: false, force: true });
    } catch (error) {
      if (task.current()) notify(error.message, "error");
    } finally {
      if (task.current()) loading = false;
    }
  }
  function renderGuide() {
    if (!Session.signedIn) return;
    account();
    layout();
    drawList();
    if (!readerKey) openArticle(selected, { remember: false });
    if (Date.now() - loadedAt > 15000) load();
  }
  function clear() {
    requests.invalidate();
    searching.invalidate();
    clearTimeout(searchTimer);
    searchHits = null;
    ++articleToken;
    loading = false;
    loadedAt = revision = 0;
    cms = [];
    categoryRows = [];
    assets = {};
    favorites.clear();
    recent = [];
    query = "";
    category = "";
    tab = "all";
    selected = "game:resource:iron";
    owner = null;
    nodes = null;
    readerKey = indexKey = "";
    root.replaceChildren();
  }
  window.addEventListener("moduleopen", (e) => {
    if (e.detail === "guide") {
      loadedAt = 0;
      renderGuide();
    }
  });
  window.addEventListener("knowledgechanged", () => {
    loadedAt = 0;
    if (Modules.isOpen("guide")) renderGuide();
  });
  window.addEventListener("gamedata", () => {
    if (
      Modules.isOpen("guide") &&
      !document.body.classList.contains("planet-mode")
    )
      renderGuide();
  });
  window.addEventListener("languagechange", () => {
    if (!Session.signedIn) return;
    requests.invalidate();
    searching.invalidate();
    searchHits = null;
    loading = false;
    nodes = null;
    loadedAt = 0;
    readerKey = indexKey = "";
    if (Modules.isOpen("guide")) renderGuide();
  });
  return {
    renderGuide,
    clear,
    open(id) {
      account();
      layout();
      openArticle(id);
      Modules.open("guide");
      load();
    },
  };
}
