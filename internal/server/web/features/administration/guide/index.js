import { Session, RequestScope } from "../../../core/session.js";
import { I18n } from "../../../core/i18n.js";
import { api } from "../../../core/api.js";
import { uuid } from "../../../core/uuid.js";
import { UI, Modules } from "../../../ui/runtime.js";
import { notify } from "../../../ui/feedback.js";
import { renderBlocks } from "../../guide/blocks.js";

export function initKnowledgeAdmin() {
  const root = Modules.create(
      "guide-admin",
      "Управление базой знаний",
      1060,
      740,
      { audience: "admin", private: true },
    ),
    n = UI.node,
    b = UI.button,
    tr = (s) => I18n.t(s),
    scope = new RequestScope();
  let nodes,
    kind = "articles",
    query = "",
    statusFilter = "",
    categories = [],
    articles = [],
    assets = [],
    media = [],
    selected = null,
    versions = [],
    publications = [],
    attached = [],
    related = [],
    busy = false;
  const valid = () =>
    Session.signedIn && Session.data.state?.user.role === "admin";
  const options = (host, items, value) => {
    const select = n("select");
    for (const [id, label] of items) {
      const option = n("option", label);
      option.value = id;
      select.append(option);
    }
    select.value = value || "";
    host.append(select);
    return select;
  };
  const field = (host, label, value = "", type = "text") => {
    const wrap = n("label"),
      input = n(type === "textarea" ? "textarea" : "input");
    if (type !== "textarea") input.type = type;
    input.value = value ?? "";
    wrap.append(n("span", tr(label)), input);
    host.append(wrap);
    return input;
  };
  function layout() {
    if (nodes && root.contains(nodes.main)) return;
    const heading = n("div", undefined, "knowledge-admin-heading"),
      main = n("div", undefined, "knowledge-admin-layout"),
      left = n("aside"),
      list = n("div", undefined, "knowledge-admin-list"),
      editor = n("section", undefined, "knowledge-admin-editor"),
      search = n("input");
    search.type = "search";
    search.placeholder = tr("Поиск");
    search.oninput = () => {
      query = search.value;
      drawList();
    };
    for (const [id, title] of [
      ["articles", "Статьи"],
      ["categories", "Категории"],
      ["media", "Изображения"],
    ]) {
      const button = b(title, () => {
        kind = id;
        selected = null;
        editor.replaceChildren();
        drawList();
      });
      button.dataset.section = id;
      heading.append(button);
    }
    heading.append(b("Обновить данные", load));
    const filter = options(
      left,
      [
        ["", tr("Все статусы")],
        ...[
          "active",
          "visible",
          "hidden",
          "archived",
          "pending",
          "ready",
          "quarantined",
          "deleted",
        ].map((value) => [value, tr(value)]),
      ],
      statusFilter,
    );
    filter.setAttribute("aria-label", tr("Фильтр статусов"));
    filter.onchange = () => {
      statusFilter = filter.value;
      drawList();
    };
    left.prepend(search);
    left.append(list);
    main.append(left, editor);
    root.replaceChildren(heading, main);
    nodes = { heading, main, list, editor };
  }
  async function load() {
    if (!valid() || busy) return;
    const task = scope.begin();
    layout();
    try {
      const data = await Promise.all([
        api("admin/guide/categories", undefined, task.signal),
        api("admin/guide/articles", undefined, task.signal),
        api("admin/guide/assets", undefined, task.signal),
        api("admin/guide/media", undefined, task.signal),
      ]);
      if (!task.current() || !valid()) return;
      [categories, articles, assets, media] = data.map((v) => v.items);
      drawList();
      if (!nodes.editor.childElementCount)
        nodes.editor.append(
          n("p", tr("Выберите материал или создайте новый"), "muted"),
        );
    } catch (e) {
      if (task.current()) notify(e.message, "error");
    }
  }
  function drawList() {
    if (!nodes) return;
    let source =
      kind === "categories" ? categories : kind === "media" ? media : articles;
    if (kind === "categories") source = categoryTree();
    nodes.list.replaceChildren(
      b(
        kind === "categories"
          ? "Создать категорию"
          : kind === "media"
            ? "Загрузить изображение"
            : "Создать статью",
        () =>
          kind === "categories"
            ? editCategory(null)
            : kind === "media"
              ? editMedia()
              : editArticle(null),
      ),
    );
    for (const item of source.filter(
      (v) =>
        (!statusFilter || (v.status || v.state) === statusFilter) &&
        String(v.title || v.alt_ru || v.slug || v.id)
          .toLocaleLowerCase()
          .includes(query.toLocaleLowerCase()),
    )) {
      const row = b("", () =>
        kind === "categories"
          ? editCategory(item)
          : kind === "media"
            ? editMedia(item)
            : editArticle(item),
      );
      row.className = "knowledge-admin-entry";
      if (kind === "categories")
        row.style.paddingInlineStart = 8 + item.depth * 12 + "px";
      row.append(
        n("strong", item.title || item.alt_ru || item.id),
        n("small", tr(item.status || item.state || "active")),
      );
      nodes.list.append(row);
    }
    for (const button of nodes.heading.querySelectorAll("[data-section]"))
      button.classList.toggle("active", button.dataset.section === kind);
  }
  function categoryTree(parent = null, depth = 0) {
    if (depth >= 3) return [];
    return categories
      .filter((item) => (item.parent_id || null) === parent)
      .flatMap((item) => [
        { ...item, depth },
        ...categoryTree(item.id, depth + 1),
      ]);
  }
  async function write(path, body) {
    if (busy || !valid()) return null;
    busy = true;
    const epoch = Session.epoch;
    for (const e of nodes.editor.querySelectorAll("button")) e.disabled = true;
    try {
      const result = await api(path, { operation: uuid(), ...body });
      if (epoch !== Session.epoch || !valid()) return null;
      notify("Изменения сохранены");
      window.dispatchEvent(new Event("knowledgechanged"));
      return result;
    } catch (e) {
      if (epoch === Session.epoch) {
        notify(e.message, "error");
        if (e.status === 409 && selected?.id) {
          const id = selected.id;
          try {
            const source = await api(
              "admin/guide/" +
                (kind === "categories" ? "categories" : "articles"),
            );
            if (epoch !== Session.epoch || selected?.id !== id) return null;
            const current = source.items.find((v) => v.id === id);
            nodes.editor.querySelector(".knowledge-conflict")?.remove();
            const conflict = n("aside", undefined, "knowledge-conflict");
            conflict.append(
              n(
                "p",
                tr("Правка не сохранена. Текущая версия:") +
                  " " +
                  (current?.version ?? "—"),
              ),
              b("Загрузить актуальную версию", async () => {
                if (
                  !(await UI.confirm(
                    "Заменить несохранённый текст актуальной версией?",
                  ))
                )
                  return;
                if (kind === "categories") editCategory(current);
                else editArticle(current);
              }),
            );
            nodes.editor.prepend(conflict);
          } catch {}
        }
      }
      return null;
    } finally {
      if (epoch === Session.epoch) {
        busy = false;
        for (const e of nodes.editor.querySelectorAll("button"))
          e.disabled = false;
      }
    }
  }
  function editCategory(item) {
    selected = item;
    const host = nodes.editor;
    host.replaceChildren(
      n("h3", tr(item ? "Изменить категорию" : "Создать категорию")),
    );
    const form = n("div", undefined, "knowledge-admin-form");
    const slug = field(form, "Код категории", item?.slug),
      ru = field(form, "Название RU", item?.title),
      en = field(form, "Название EN", item?.en_title),
      description = field(form, "Описание RU", item?.description, "textarea"),
      enDescription = field(
        form,
        "Описание EN",
        item?.en_description,
        "textarea",
      ),
      order = field(form, "Порядок", item?.sort_order || 0, "number");
    const parent = options(
        form,
        [
          ["", tr("Корневая категория")],
          ...categories
            .filter((v) => v.id !== item?.id)
            .map((v) => [v.id, v.title]),
        ],
        item?.parent_id,
      ),
      status = options(
        form,
        [
          ["visible", tr("Видима")],
          ["hidden", tr("Скрыта")],
          ["archived", tr("В архиве")],
        ],
        item?.status || "visible",
      );
    host.append(form);
    const payload = () => ({
      id: item?.id || "",
      expected_revision: item?.version || 0,
      action: "save",
      slug: slug.value,
      ru: { title: ru.value, description: description.value },
      en: { title: en.value, description: enDescription.value },
      parent: parent.value || null,
      order: Number(order.value),
      status: status.value,
    });
    host.append(
      b("Сохранить категорию", async () => {
        const result = await write("admin/guide/categories", payload());
        if (result) {
          selected = null;
          host.replaceChildren();
          await load();
          editCategory(categories.find((v) => v.id === result.id));
        }
      }),
    );
    if (item)
      host.append(
        b("Удалить пустую категорию", async () => {
          if (!(await UI.confirm("Удалить пустую категорию?"))) return;
          const result = await write("admin/guide/categories", {
            operation: uuid(),
            id: item.id,
            expected_revision: item.version,
            action: "delete",
          });
          if (result) {
            host.replaceChildren();
            await load();
          }
        }),
      );
  }
  async function editArticle(item) {
    selected = item;
    versions = [];
    publications = [];
    attached = [];
    related = [];
    const task = scope.begin();
    const host = nodes.editor;
    host.replaceChildren(
      n("h3", tr(item ? "Редактор статьи" : "Создать статью")),
    );
    if (item)
      try {
        const data = await api(
          "admin/guide/revisions?article_id=" + item.id,
          undefined,
          task.signal,
        );
        if (!task.current() || selected?.id !== item.id) return;
        versions = data.items;
        publications = data.publications;
        related = (data.related || []).map((v) => v.related_id);
      } catch (e) {
        if (task.current()) notify(e.message, "error");
        return;
      }
    const form = n("div", undefined, "knowledge-admin-form"),
      slug = field(form, "Код статьи", item?.slug),
      category = options(
        form,
        categories.map((v) => [v.id, v.title]),
        item?.category_id,
      ),
      locale = options(
        form,
        [
          ["ru", "RU"],
          ["en", "EN"],
        ],
        I18n.language,
      ),
      title = field(form, "Заголовок"),
      summary = field(form, "Аннотация", "", "textarea"),
      markdown = field(form, "Markdown", "", "textarea");
    markdown.className = "knowledge-markdown";
    markdown.placeholder = tr("## Заголовок, текст и ссылки article: / game:");
    const order = field(form, "Порядок", item?.sort_order || 0, "number"),
      attachments = n("div", undefined, "knowledge-attachments"),
      relatedBox = n("div", undefined, "knowledge-attachments"),
      preview = n("article", undefined, "knowledge-preview"),
      actions = n("div", undefined, "knowledge-admin-actions"),
      history = n("details"),
      historyList = n("div");
    history.append(
      n("summary", tr("История редакций и публикаций")),
      historyList,
    );
    host.append(form, attachments, relatedBox, actions, preview, history);
    function attachmentList() {
      attachments.replaceChildren(n("h4", tr("Иллюстрации")));
      const source = options(
          attachments,
          [
            ["", tr("Выберите изображение")],
            ...assets.map((v) => ["builtin:" + v.key, v.key]),
            ...media
              .filter((v) => v.state === "ready")
              .map((v) => ["media:" + v.id, v.alt_ru || v.id]),
          ],
          "",
        ),
        role = options(
          attachments,
          [
            ["illustration", tr("Иллюстрация")],
            ["cover", tr("Обложка")],
            ["hologram", tr("Голограмма")],
          ],
          "illustration",
        );
      attachments.append(
        b("Добавить изображение", () => {
          if (!source.value) return;
          const isBuiltin = source.value.startsWith("builtin:");
          attached.push({
            builtin: isBuiltin ? source.value.slice(8) : "",
            media: isBuiltin ? "" : source.value.slice(6),
            role: role.value,
            caption: "",
          });
          attachmentList();
        }),
      );
      attached.forEach((v, i) => {
        const row = n("div");
        const caption = n("input");
        caption.value = v.caption;
        caption.maxLength = 500;
        caption.placeholder = tr("Подпись изображения");
        caption.setAttribute("aria-label", tr("Подпись изображения"));
        caption.oninput = () => {
          v.caption = caption.value;
        };
        const move = (delta) => {
          const next = i + delta;
          [attached[i], attached[next]] = [attached[next], attached[i]];
          attachmentList();
        };
        row.append(
          n("span", v.builtin || v.media),
          caption,
          b("Выше", () => move(-1), i === 0),
          b("Ниже", () => move(1), i === attached.length - 1),
          b("Убрать", () => {
            attached.splice(i, 1);
            attachmentList();
          }),
        );
        attachments.append(row);
      });
    }
    function relatedList() {
      relatedBox.replaceChildren(n("h4", tr("Связанные материалы")));
      const select = options(
        relatedBox,
        [
          ["", tr("Выберите статью")],
          ...articles
            .filter((v) => v.id !== item?.id && !related.includes(v.id))
            .map((v) => [v.id, v.title]),
        ],
        "",
      );
      relatedBox.append(
        b("Добавить связь", () => {
          if (select.value) {
            related.push(select.value);
            relatedList();
          }
        }),
      );
      related.forEach((id) => {
        const row = n("div");
        row.append(
          n("span", articles.find((v) => v.id === id)?.title || id),
          b("Убрать", () => {
            related = related.filter((v) => v !== id);
            relatedList();
          }),
        );
        relatedBox.append(row);
      });
    }
    function fill() {
      const v = versions.find((v) => v.locale === locale.value);
      title.value = v?.title || "";
      summary.value = v?.summary || "";
      markdown.value = v?.markdown_source || "";
      attached = (v?.media || []).map((v) => ({
        builtin: v.builtin_asset_key || "",
        media: v.uploaded_media_id || "",
        role: v.role,
        caption: v.caption,
      }));
      attachmentList();
    }
    locale.onchange = fill;
    fill();
    relatedList();
    const save = b("Сохранить черновик", async () => {
      const result = await write("admin/guide/articles", {
        action: "save",
        id: item?.id || "",
        expected_revision: item?.version || 0,
        slug: slug.value,
        category: category.value,
        order: Number(order.value),
        locale: locale.value,
        title: title.value,
        summary: summary.value,
        markdown: markdown.value,
        media: attached,
        related,
      });
      if (result) {
        host.replaceChildren();
        await load();
        await editArticle(articles.find((v) => v.id === result.id));
      }
    });
    actions.append(
      save,
      b("Предпросмотр", async () => {
        const epoch = Session.epoch;
        try {
          const data = await api("admin/guide/preview", {
            markdown: markdown.value,
          });
          if (epoch !== Session.epoch || !valid() || !preview.isConnected)
            return;
          preview.replaceChildren();
          renderBlocks(preview, data.blocks, {
            assets: Object.fromEntries(assets.map((v) => [v.key, v])),
          });
        } catch (e) {
          if (epoch === Session.epoch) notify(e.message, "error");
        }
      }),
    );
    if (item) {
      for (const action of ["publish", "unpublish"]) {
        actions.append(
          b(
            action === "publish" ? "Опубликовать" : "Снять публикацию",
            async () => {
              const revision = versions.find(
                (v) => v.locale === locale.value,
              )?.revision;
              if (action === "publish" && !revision) {
                notify("Сначала сохраните перевод", "warning");
                return;
              }
              const result = await write("admin/guide/publish", {
                action,
                id: item.id,
                expected_revision: item.version,
                locale: locale.value,
                revision: revision || 0,
              });
              if (result) {
                host.replaceChildren();
                await load();
                await editArticle(articles.find((v) => v.id === result.id));
              }
            },
          ),
        );
      }
      actions.append(
        b(
          item.status === "archived"
            ? "Восстановить статью"
            : "Архивировать статью",
          async () => {
            const result = await write("admin/guide/articles", {
              id: item.id,
              expected_revision: item.version,
              action: item.status === "archived" ? "restore" : "archive",
            });
            if (result) {
              host.replaceChildren();
              await load();
              await editArticle(articles.find((v) => v.id === item.id));
            }
          },
        ),
      );
      if (item.status !== "archived")
        actions.append(
          b(
            item.status === "hidden" ? "Показать статью" : "Скрыть статью",
            async () => {
              const result = await write("admin/guide/articles", {
                id: item.id,
                expected_revision: item.version,
                action: item.status === "hidden" ? "unhide" : "hide",
              });
              if (result) {
                host.replaceChildren();
                await load();
                await editArticle(articles.find((v) => v.id === item.id));
              }
            },
          ),
        );
    }
    for (const v of versions) {
      const row = n("section"),
        label = n(
          "strong",
          v.locale.toUpperCase() + " · " + v.revision + " · " + v.title,
        ),
        source = n("pre", v.markdown_source);
      source.hidden = true;
      row.append(
        label,
        n(
          "span",
          publications.some(
            (p) => p.locale === v.locale && p.revision === v.revision,
          )
            ? tr("Опубликовано")
            : tr("Черновик"),
        ),
        b("Просмотреть редакцию", () => {
          source.hidden = !source.hidden;
        }),
        source,
        b("Сравнить с текстом редактора", () => {
          source.hidden = false;
          const before = v.markdown_source.split("\n"),
            after = markdown.value.split("\n");
          source.replaceChildren(
            n("small", tr("− сохранённая редакция · + текст редактора")),
          );
          for (let i = 0; i < Math.max(before.length, after.length); i++) {
            if (before[i] === after[i])
              source.append(n("div", "  " + before[i]));
            else {
              if (before[i] !== undefined)
                source.append(
                  n("div", "− " + before[i], "knowledge-diff-removed"),
                );
              if (after[i] !== undefined)
                source.append(
                  n("div", "+ " + after[i], "knowledge-diff-added"),
                );
            }
          }
        }),
        b("Восстановить как новую редакцию", async () => {
          const result = await write("admin/guide/articles", {
            id: item.id,
            expected_revision: item.version,
            action: "restore_revision",
            locale: v.locale,
            revision: v.revision,
          });
          if (result) {
            host.replaceChildren();
            await load();
            await editArticle(articles.find((a) => a.id === item.id));
          }
        }),
      );
      historyList.append(row);
    }
  }
  function editMedia(item) {
    const host = nodes.editor;
    host.replaceChildren(
      n("h3", tr("Изображения базы знаний")),
      n(
        "p",
        tr(
          "PNG, JPEG или WebP до 8 MiB и 4096 × 4096. SVG можно выбирать из встроенного каталога.",
        ),
        "muted",
      ),
    );
    if (item) {
      host.append(n("p", item.alt_ru || item.id));
      if (item.state === "ready") {
        const img = n("img");
        img.src = "/api/guide/media/" + item.id;
        img.alt = item.alt_ru || "";
        img.className = "guide-illustration";
        host.append(
          img,
          b("Удалить неиспользуемое изображение", async () => {
            const result = await write("admin/guide/media/delete", {
              id: item.id,
              action: "delete",
            });
            if (result) {
              host.replaceChildren();
              await load();
            }
          }),
        );
      }
      return;
    }
    const file = field(host, "Файл", "", "file");
    file.accept = "image/png,image/jpeg,image/webp";
    const ru = field(host, "Подпись RU"),
      en = field(host, "Подпись EN");
    host.append(
      b("Загрузить", async () => {
        if (busy || !file.files[0]) return;
        busy = true;
        const task = scope.begin();
        try {
          const form = new FormData();
          form.append("operation", uuid());
          form.append("file", file.files[0]);
          form.append("alt_ru", ru.value);
          form.append("alt_en", en.value);
          const response = await fetch("/api/admin/guide/media", {
            method: "POST",
            body: form,
            signal: AbortSignal.any([task.signal, AbortSignal.timeout(30000)]),
          });
          const data = await response.json();
          if (!task.current()) return;
          if (!response.ok) throw Error(tr(data.error));
          busy = false;
          host.replaceChildren();
          await load();
          notify("Изображение загружено");
        } catch (e) {
          if (task.current()) notify(e.message, "error");
        } finally {
          if (task.current()) busy = false;
        }
      }),
    );
  }
  function clear() {
    scope.invalidate();
    busy = false;
    selected = null;
    nodes = null;
    articles =
      categories =
      assets =
      media =
      versions =
      publications =
      attached =
      related =
        [];
    root.replaceChildren();
    Modules.close("guide-admin");
  }
  window.addEventListener("moduleopen", (e) => {
    if (e.detail === "guide-admin" && valid()) {
      layout();
      load();
    }
  });
  window.addEventListener("signedout", clear);
  window.addEventListener("gamedata", () => {
    if (!valid()) clear();
  });
  window.addEventListener("languagechange", () => {
    if (Modules.isOpen("guide-admin")) {
      nodes = null;
      layout();
      load();
    }
  });
}
