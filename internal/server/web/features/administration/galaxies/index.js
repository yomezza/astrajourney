import { Format } from "../../../core/format.js";
import { Session } from "../../../core/session.js";
import { I18n } from "../../../core/i18n.js";
import { api } from "../../../core/api.js";
import { refresh } from "../../../core/snapshot-store.js";
import { notify } from "../../../ui/feedback.js";
import { UI, Modules } from "../../../ui/runtime.js";
import { GalaxyAdmin } from "../../runtime.js";
import { world } from "../../../core/world-store.js";

export function initGalaxyAdmin() {
  const n = UI.node,
    tr = (s) => I18n.t(s);
  let selected = "edem",
    request = 0,
    logRequest = 0,
    draft = null;
  const edits = new Map();
  const admin = () =>
    Session.signedIn && Session.data.state?.user.role === "admin";
  const labels = {
    systems: "Системы",
    lanes: "Гиперкоридоры",
    cross_links: "Связи между рукавами",
    station_systems: "Системы со станциями",
    stations: "Станции",
    founder_systems: "Системы основателей",
    neutral_systems: "Нейтральные системы",
  };
  const actions = {
    tick: "Пропустить тик",
    gameplay: "Игровой процесс",
    user_edit: "Редактирование пользователя",
    teleport: "Телепортация",
    planet_surface_reset: "Сброс поверхности планеты",
    galaxy_update: "Изменение галактики",
    galaxy_delete: "Удаление галактики",
  };
  Object.assign(GalaxyAdmin, {
    async render(root) {
      if (!admin()) {
        root.replaceChildren();
        return false;
      }
      const key = ++request,
        epoch = Session.epoch,
        result = await api("admin/galaxies");
      if (
        key !== request ||
        epoch !== Session.epoch ||
        !admin() ||
        !Modules.isOpen("galaxies")
      )
        return false;
      root.replaceChildren();
      root.classList.add("galaxy-admin");
      if (draft) result.galaxies.push(draft);
      const create = UI.button(
        draft ? "К черновику" : "Создать галактику",
        () => {
          if (draft) {
            selected = draft.definition.id;
            return GalaxyAdmin.render(root);
          }
          draft = {
            definition: {
              id: "g-" + Date.now().toString(36),
              name: "",
              shape: "spiral",
              seed: Date.now(),
              opened: true,
              create: true,
              settings: structuredClone(result.galaxies[0].definition.settings),
            },
            stats: Object.fromEntries(Object.keys(labels).map((k) => [k, 0])),
          };
          selected = draft.definition.id;
          root.replaceChildren(n("p", tr("Загрузка…")));
          return GalaxyAdmin.render(root);
        },
      );
      const header = n("div", undefined, "galaxy-admin-heading"),
        title = n("div");
      title.append(
        n("p", tr("Управление вселенной"), "eyebrow"),
        n("h3", tr("Галактики")),
        n(
          "p",
          tr("Настройте генерацию, проверьте карту и примените результат."),
          "muted",
        ),
      );
      create.classList.add("galaxy-create-button");
      header.append(title, create);
      root.append(header);
      const layout = n("div", undefined, "galaxy-admin-layout"),
        list = n("aside", undefined, "galaxy-admin-list"),
        editor = n("section", undefined, "galaxy-admin-editor");
      layout.append(list, editor);
      root.append(layout);
      if (!result.galaxies.some((g) => g.definition.id === selected))
        selected = result.galaxies[0]?.definition.id;
      list.append(
        n(
          "p",
          tr("Каталог галактик") + " · " + result.galaxies.length,
          "galaxy-list-caption",
        ),
      );
      for (const entry of result.galaxies) {
        const d = entry.definition,
          card = UI.button("", () => {
            selected = d.id;
            editor.replaceChildren(n("p", tr("Загрузка…"), "muted"));
            return GalaxyAdmin.render(root);
          });
        card.classList.add("galaxy-admin-card");
        card.dataset.galaxy = d.id;
        card.classList.toggle("selected", d.id === selected);
        card.append(
          n(
            "span",
            { spiral: "✺", rings: "◎", elliptical: "⬭", irregular: "✦" }[
              d.shape
            ] || "✦",
            "galaxy-card-symbol",
          ),
          n("b", d.name || tr("Новая галактика")),
          n(
            "small",
            tr(
              {
                rings: "Кольцевая",
                spiral: "Спиральная",
                elliptical: "Эллиптическая",
                irregular: "Нерегулярная",
              }[d.shape],
            ),
          ),
          n(
            "span",
            d.create
              ? tr("Черновик")
              : d.opened
                ? `${Format.quantity(entry.stats.systems)} ${tr("систем")}`
                : tr("Галактика не открыта"),
          ),
        );
        list.append(card);
      }
      const entry = result.galaxies.find((g) => g.definition.id === selected);
      if (!entry) return;
      const d = entry.definition;
      if (edits.has(d.id)) Object.assign(d, structuredClone(edits.get(d.id)));
      const editorHeader = n("div", undefined, "galaxy-editor-heading"),
        editorTitle = n("div");
      editorTitle.append(
        n(
          "p",
          tr(d.create ? "Новая галактика" : "Параметры галактики"),
          "eyebrow",
        ),
        n("h2", d.name || tr("Новая галактика")),
      );
      const badge = n(
        "span",
        tr(d.create ? "Черновик" : d.opened ? "Доступна" : "Не открыта"),
        "galaxy-state-tag",
      );
      badge.dataset.state = d.create ? "draft" : d.opened ? "open" : "closed";
      editorHeader.append(editorTitle, badge);
      editor.append(editorHeader);
      const summary = n("div", undefined, "galaxy-stats");
      for (const id of ["systems", "lanes", "stations", "neutral_systems"]) {
        const metric = n("div");
        metric.append(
          n("strong", Format.quantity(entry.stats[id])),
          n("span", tr(labels[id])),
        );
        summary.append(metric);
      }
      editor.append(summary);
      if (!d.create && d.id !== "edem" && d.id !== "asterion")
        editor.append(
          UI.button("Удалить галактику", async () => {
            const review = await api("admin/galaxies/delete-preview", {
              id: d.id,
            });
            if (epoch !== Session.epoch || !admin()) return;
            const box = Modules.create(
              "galaxy-delete",
              "Удаление галактики",
              600,
              530,
            );
            box.replaceChildren(
              n("h3", d.name),
              n("p", tr("Системы") + ": " + review.systems),
            );
            const names = {
              ships: "Корабли и маршруты",
              warehouses: "Личные склады",
              corporate_storage: "Корпоративные запасы",
              stations: "Станции игроков",
              projects: "Корпоративные проекты",
              planet_projects: "Планетарные проекты",
              tasks: "Задачи планировщика",
              deliveries: "Доставки",
              reservations: "Бронирования",
              offers: "Рыночные предложения",
              research: "Исследование и добыча",
              player_data: "Данные игроков",
            };
            const blocked = Object.entries(review.blockers).filter(
              ([, v]) => v > 0,
            );
            box.append(
              n(
                "p",
                tr(
                  review.can_delete
                    ? "Галактика будет удалена с карт и переходов. Остальные галактики сохранятся."
                    : "Сначала освободите галактику от кораблей, имущества и данных игроков.",
                ),
                "muted",
              ),
            );
            if (blocked.length)
              box.append(
                UI.table(
                  ["Причина", "Количество"],
                  blocked.map(([k, v]) => [tr(names[k] || k), v]),
                ),
              );
            box.append(
              UI.button(
                "Удалить галактику",
                async () => {
                  await api("admin/galaxies/delete", {
                    id: d.id,
                    preview: review.preview,
                  });
                  Modules.close("galaxy-delete");
                  selected = "edem";
                  world.data = await api("galaxy");
                  window.dispatchEvent(new Event("universechange"));
                  await refresh(true);
                  await GalaxyAdmin.render(root);
                  notify(tr("Галактика удалена"));
                },
                !review.can_delete,
              ),
              UI.button("Отмена", () => Modules.close("galaxy-delete")),
            );
            Modules.open("galaxy-delete");
          }),
        );
      if (!d.opened) {
        UI.empty(editor, "Галактика откроется после активации портала");
        return;
      }
      const form = n("form"),
        basic = n("section", undefined, "galaxy-config-section"),
        grid = n("div", undefined, "galaxy-fields");
      form.classList.add("galaxy-config-form");
      basic.append(n("h3", tr("Основные параметры")), grid);
      form.onsubmit = (e) => e.preventDefault();
      form.append(basic);
      editor.append(form);
      const name = UI.field(grid, "Название галактики", "galaxy-name", d.name);
      name.maxLength = 48;
      name.required = true;
      const count = UI.field(
        grid,
        "Количество систем",
        "galaxy-count",
        d.systems ?? (d.create ? 1000 : entry.stats.systems),
        "number",
      );
      count.min = d.create ? 5 : entry.stats.systems;
      count.max = 50000;
      count.required = true;
      if (d.create) {
        const shapes = n("div", undefined, "galaxy-shapes");
        shapes.setAttribute("role", "radiogroup");
        shapes.setAttribute("aria-label", tr("Форма галактики"));
        for (const [value, label, symbol] of [
          ["spiral", "Спиральная", "✺"],
          ["rings", "Кольцевая", "◎"],
          ["elliptical", "Эллиптическая", "⬭"],
          ["irregular", "Нерегулярная", "✦"],
        ]) {
          const choice = UI.button("", () => {
            d.shape = value;
            for (const item of shapes.children)
              item.setAttribute(
                "aria-checked",
                String(item.dataset.shape === value),
              );
            form.dispatchEvent(new Event("input", { bubbles: true }));
          });
          choice.dataset.shape = value;
          choice.setAttribute("aria-label", tr(label));
          choice.setAttribute("role", "radio");
          choice.setAttribute("aria-checked", String(d.shape === value));
          choice.append(
            n("span", symbol, "galaxy-shape-symbol"),
            n("span", tr(label)),
          );
          shapes.append(choice);
        }
        basic.append(
          n("p", tr("Форма галактики"), "galaxy-section-caption"),
          shapes,
        );
        const seed = UI.field(
          grid,
          "Зерно генерации",
          "galaxy-seed",
          d.seed,
          "number",
        );
        seed.required = true;
        seed.max = Number.MAX_SAFE_INTEGER;
        seed.oninput = () => (d.seed = Number(seed.value));
      }
      basic.append(
        n(
          "p",
          tr(
            d.create
              ? "От 5 до 50 000 систем. Одинаковое зерно и параметры дают одинаковую карту."
              : "Можно расширить галактику; существующие системы сохранят свои координаты и названия.",
          ),
          "muted",
        ),
      );
      const fields = {};
      const section = (title) => {
        const details = n("details", undefined, "galaxy-config-section");
        details.open = title !== "Распределение ресурсов";
        details.append(n("summary", tr(title)));
        const box = n("div", undefined, "galaxy-fields");
        details.append(box);
        form.append(details);
        return box;
      };
      const lanes = section("Гиперкоридоры");
      for (const [id, label, min, max, help] of [
        [
          "extra_lanes",
          "Дополнительные коридоры, %",
          0,
          100,
          "После коротких основных связей добавляются альтернативные пути. Меньшее значение делает сеть разреженной.",
        ],
        [
          "cross_links",
          "Связи между рукавами",
          0,
          40,
          "Соединяются ближайшие системы соседних рукавов в пределах максимальной длины коридора.",
        ],
        [
          "max_lane_length",
          "Максимальная длина коридора",
          50,
          10000,
          "Малое значение может разделить сеть на несколько областей. Действующие маршруты и два учебных коридора сохраняются.",
        ],
      ]) {
        const field = UI.field(
          lanes,
          label,
          "galaxy-" + id,
          d.settings[id],
          "number",
          help,
        );
        field.min = min;
        field.max = max;
        field.required = true;
        fields[id] = field;
      }
      const ports = section("Распределение станций");
      for (const [id, label, min, max] of [
        ["station_percent", "Частота станций в центре, %", 0, 100],
        ["max_ports", "Максимум портов в системе", 1, 5],
      ]) {
        const field = UI.field(
          ports,
          label,
          "galaxy-" + id,
          d.settings[id],
          "number",
        );
        field.min = min;
        field.max = max;
        field.required = true;
        fields[id] = field;
      }
      ports.append(
        n(
          "p",
          tr(
            "К окраинам частота уменьшается до нуля. Владения основателей — небольшие колонии вокруг станций; между ними остаются нейтральные системы.",
          ),
          "muted",
        ),
      );
      ports.append(
        n(
          "p",
          tr(
            "Стартовые порты, порты у чёрной дыры и станции с имуществом или действующими заказами сохраняются.",
          ),
          "muted",
        ),
      );
      const weights = { ...d.settings.resource_weights },
        resourceFields = {},
        res = section("Распределение ресурсов");
      res.append(
        n(
          "p",
          tr(
            "100% — базовая распространённость, 0% — новые месторождения этого сырья отсутствуют. Запасы в трюмах, на складах и рынках сохраняются.",
          ),
          "muted",
        ),
      );
      // The expedition catalogue also contains technologies, e.g. colony_core.
      // Only submit keys defined by the server's galaxy settings contract.
      const generationResources = Session.data.expedition.resources.filter(
        (r) =>
          Object.hasOwn(weights, r.id) &&
          !["adamant", "electrum", "nectar", "soma"].includes(r.id),
      );
      const resourceTools = n("div", undefined, "galaxy-resource-tools"),
        resourceSearch = n("input");
      resourceSearch.type = "search";
      resourceSearch.placeholder = tr("Поиск сырья");
      resourceSearch.setAttribute("aria-label", tr("Поиск сырья"));
      resourceTools.append(
        resourceSearch,
        UI.button("Вернуть 100%", () => {
          for (const field of Object.values(resourceFields))
            field.value = "100";
          form.dispatchEvent(new Event("input", { bubbles: true }));
        }),
      );
      res.prepend(resourceTools);
      res.classList.add("galaxy-resource-grid");
      const filterResources = () => {
        const q = resourceSearch.value.trim().toLocaleLowerCase(I18n.language);
        for (const field of Object.values(resourceFields))
          field.closest("label").hidden = !field
            .closest("label")
            .textContent.toLocaleLowerCase(I18n.language)
            .includes(q);
      };
      resourceSearch.oninput = (event) => {
        event.stopPropagation();
        filterResources();
      };
      for (const resource of generationResources) {
        const f = UI.field(
          res,
          tr(resource.name) + " · %",
          "galaxy-resource-" + resource.id,
          weights[resource.id],
          "number",
        );
        f.min = 0;
        f.max = 300;
        f.required = true;
        resourceFields[resource.id] = f;
      }
      const controls = n("div", undefined, "action-row galaxy-editor-actions"),
        preview = n("section", undefined, "galaxy-preview"),
        status = n("p", undefined, "galaxy-editor-status");
      status.setAttribute("role", "status");
      const setStatus = (text, state = "idle") => {
        status.textContent = tr(text);
        status.dataset.state = state;
      };
      setStatus("Сначала настройте параметры, затем сформируйте предпросмотр.");
      preview.append(
        n("div", "✺", "galaxy-preview-symbol"),
        n("h3", tr("Предпросмотр карты")),
        n(
          "p",
          tr(
            "Здесь появятся карта и сравнение параметров. Игровой мир изменится только после применения.",
          ),
          "muted",
        ),
      );
      const validForm = () => {
        const invalid = [...form.elements].find(
          (field) => field.willValidate && !field.validity.valid,
        );
        if (!invalid) return true;
        showEditorTab("settings");
        const details = invalid.closest("details");
        if (details) details.open = true;
        resourceSearch.value = "";
        filterResources();
        invalid.closest("label")?.classList.add("field-invalid");
        setStatus("Проверьте выделенное поле", "error");
        invalid.reportValidity();
        return false;
      };
      let snapshot = null,
        dirty = 0;
      const values = () => ({
        id: d.id,
        create: !!d.create,
        shape: d.shape,
        seed: d.seed,
        name: name.value,
        systems: Number(count.value),
        settings: {
          ...d.settings,
          ...Object.fromEntries(
            Object.entries(fields).map(([id, f]) => [id, Number(f.value)]),
          ),
          resource_weights: {
            ...weights,
            ...Object.fromEntries(
              Object.entries(resourceFields).map(([id, f]) => [
                id,
                Number(f.value),
              ]),
            ),
          },
        },
      });
      const apply = UI.button(
        d.create ? "Создать галактику" : "Применить изменения",
        async () => {
          if (!snapshot || !validForm()) return;
          const body = { ...values(), preview: snapshot.preview };
          setStatus("Применяем изменения…", "loading");
          try {
            await api("admin/galaxies/apply", body);
          } catch (error) {
            if (error.status === 409) {
              snapshot = null;
              apply.disabled = true;
            }
            setStatus(error.message, "error");
            throw error;
          }
          if (epoch !== Session.epoch || !admin()) return;
          selected = d.id;
          if (draft?.definition.id === d.id) draft = null;
          edits.delete(d.id);
          notify(d.create ? "Галактика создана" : "Галактика обновлена");
          world.data = await api("galaxy");
          window.dispatchEvent(new Event("universechange"));
          await refresh(true);
          await GalaxyAdmin.render(root);
        },
        true,
      );
      // A preview token is valid only for this exact set of form values.
      const previewButton = UI.button("Предпросмотр", async () => {
        if (!validForm()) return;
        setStatus("Формируем предпросмотр…", "loading");
        const current = dirty;
        let proposal;
        try {
          proposal = await api("admin/galaxies/preview", values());
        } catch (error) {
          setStatus(error.message, "error");
          throw error;
        }
        if (
          current !== dirty ||
          key !== request ||
          epoch !== Session.epoch ||
          !admin()
        )
          return;
        snapshot = proposal;
        preview.replaceChildren(n("h3", tr("Предпросмотр")));
        const comparison = n("details", undefined, "galaxy-preview-comparison");
        comparison.append(n("summary", tr("Сводка изменений")));
        comparison.append(
          UI.table(
            ["Параметр", "Сейчас", "После изменения"],
            Object.entries(labels).map(([id, label]) => [
              tr(label),
              proposal.before[id],
              proposal.after[id],
            ]),
          ),
        );
        const canvas = n("canvas");
        canvas.width = 1000;
        canvas.height = 560;
        canvas.className = "galaxy-preview-map";
        preview.append(canvas);
        const ctx = canvas.getContext("2d"),
          scale = 250 / proposal.radius,
          points = new Map(proposal.stars.map((s) => [s.id, s]));
        for (const area of proposal.territories || []) {
          const center = points.get(area.anchor),
            owner = world.data?.founders?.find(
              (c) => c.founder_key === area.founder,
            );
          if (!center || !owner) continue;
          ctx.beginPath();
          ctx.arc(
            500 + center.x * scale,
            280 + center.y * scale,
            area.radius * scale,
            0,
            Math.PI * 2,
          );
          ctx.fillStyle = owner.color;
          ctx.globalAlpha = 0.1;
          ctx.fill();
          ctx.strokeStyle = owner.color;
          ctx.globalAlpha = 0.3;
          ctx.stroke();
        }
        ctx.globalAlpha = 1;
        ctx.strokeStyle = "#324657";
        ctx.lineWidth = 0.55;
        ctx.beginPath();
        for (const lane of proposal.lanes) {
          const a = points.get(lane.from),
            b = points.get(lane.to);
          ctx.moveTo(500 + a.x * scale, 280 + a.y * scale);
          ctx.lineTo(500 + b.x * scale, 280 + b.y * scale);
        }
        ctx.stroke();
        for (const star of proposal.stars) {
          ctx.fillStyle =
            star.id > (world.data?.stars.length || 0) ? "#efad58" : "#9db4c4";
          ctx.beginPath();
          ctx.arc(
            500 + star.x * scale,
            280 + star.y * scale,
            1.45,
            0,
            Math.PI * 2,
          );
          ctx.fill();
        }
        preview.append(
          n(
            "p",
            tr(
              "Янтарные точки — новые системы. Предпросмотр не меняет игровой мир.",
            ),
            "muted",
          ),
        );
        comparison.append(
          UI.table(
            ["Ресурс", "Систем с ресурсом до", "После изменения"],
            Session.data.expedition.resources
              .filter((r) => resourceFields[r.id])
              .map((r) => [
                tr(r.name),
                proposal.before.resources[r.id] || 0,
                proposal.after.resources[r.id] || 0,
              ]),
          ),
        );
        preview.append(comparison);
        apply.disabled = false;
        preview.classList.add("galaxy-preview-ready");
        showEditorTab("preview");
        editor.scrollTop = 0;
        setStatus(
          "Предпросмотр готов. Проверьте карту и подтвердите применение.",
          "ready",
        );
      });
      form.oninput = () => {
        dirty++;
        snapshot = null;
        apply.disabled = true;
        preview.replaceChildren(
          n(
            "p",
            tr("Параметры изменены — сформируйте предпросмотр заново."),
            "muted",
          ),
        );
        form.dataset.dirty = "true";
        edits.set(d.id, values());
        editorTitle.querySelector("h2").textContent =
          name.value.trim() || tr("Новая галактика");
        for (const card of list.querySelectorAll(".galaxy-admin-card"))
          if (card.dataset.galaxy === d.id)
            card.querySelector("b").textContent =
              name.value.trim() || tr("Новая галактика");
        if (draft?.definition.id === d.id)
          Object.assign(draft.definition, structuredClone(values()));
        for (const field of form.querySelectorAll("input"))
          if (field.validity.valid)
            field.closest("label")?.classList.remove("field-invalid");
        setStatus("Есть изменения. Сформируйте новый предпросмотр.", "dirty");
        preview.classList.remove("galaxy-preview-ready");
      };
      const reset = UI.button(
        d.create ? "Отменить создание" : "Сбросить изменения",
        () => {
          edits.delete(d.id);
          if (d.create) {
            draft = null;
            selected = "edem";
          }
          return GalaxyAdmin.render(root);
        },
      );
      const tabbar = n("div", undefined, "galaxy-editor-tabs");
      const showEditorTab = (value) => {
        form.hidden = value !== "settings";
        preview.hidden = value !== "preview";
        tabbar.replaceChildren();
        UI.tabs(
          tabbar,
          [
            ["settings", "Параметры"],
            ["preview", "Предпросмотр"],
          ],
          value,
          showEditorTab,
        );
      };
      controls.append(previewButton, apply, reset);
      form.before(tabbar);
      editor.append(preview, status, controls);
      showEditorTab("settings");
    },
    async logs(root) {
      if (!admin()) {
        root.replaceChildren();
        return false;
      }
      root.replaceChildren();
      const epoch = Session.epoch,
        bar = n("div", undefined, "admin-log-toolbar"),
        search = n("input"),
        rows = n("section");
      search.type = "search";
      search.placeholder = tr("Поиск по администратору, действию или данным");
      search.maxLength = 200;
      search.id = "admin-log-search";
      let page = 1,
        action = "",
        timer;
      const picker = UI.picker(
        [["", "Все действия"], ...Object.entries(actions)],
        "",
        (value) => {
          action = value;
          page = 1;
          load();
        },
      );
      const reload = UI.button("Обновить журнал", () => load());
      bar.append(search, picker, reload);
      root.append(bar, rows);
      const load = async () => {
        const key = ++logRequest;
        try {
          const result = await api(
            "admin/changes?" +
              new URLSearchParams({ page, search: search.value, action }),
          );
          if (
            key !== logRequest ||
            epoch !== Session.epoch ||
            !admin() ||
            !Modules.isOpen("adminchanges")
          )
            return;
          rows.replaceChildren();
          rows.append(n("p", tr("Записей") + ": " + result.total, "muted"));
          for (const item of result.items) {
            const entry = n("details", undefined, "admin-log-entry"),
              summary = n("summary"),
              date = Format.timestamp(item.created_at);
            summary.append(
              n("time", date),
              n("b", item.actor_name),
              n("span", tr(actions[item.action] || item.action)),
            );
            entry.append(summary);
            const data = item.data || {};
            if (item.action === "galaxy_update") {
              UI.row(entry, "Галактика", data.name || data.galaxy);
              entry.append(
                UI.table(
                  ["Параметр", "До изменения", "После изменения"],
                  Object.entries(labels).map(([key, label]) => [
                    tr(label),
                    data.before?.[key] ?? "—",
                    data.after?.[key] ?? "—",
                  ]),
                ),
              );
            } else if (item.action === "planet_surface_reset") {
              const before = data.before || {};
              UI.row(entry, "Планета", before.name || data.planet || "—");
              UI.row(
                entry,
                "Операция",
                tr(
                  data.operation === "delete"
                    ? "Удалить генерацию колонии"
                    : "Перегенерировать колонию",
                ),
              );
              UI.row(
                entry,
                "Результат",
                tr(
                  {
                    success: "Успешно",
                    blocked: "Заблокировано",
                    error: "Ошибка",
                  }[data.result] || data.result,
                ),
              );
              if (data.reason) entry.append(n("p", tr(data.reason)));
              if (data.before) {
                UI.row(entry, "Количество секторов", before.sectors);
                UI.row(entry, "Проекты", before.projects);
                UI.row(entry, "Колонии", before.colonies);
              }
            } else if (item.action === "teleport") {
              UI.row(entry, "Корабль", "#" + data.ship);
              for (const [label, id] of [
                ["Отправление", data.from],
                ["Назначение", data.to],
              ]) {
                const name = world.data?.stars[id - 1]?.name || "#" + id;
                UI.row(
                  entry,
                  label,
                  UI.button(name, () => {
                    Modules.open("galaxy");
                    window.dispatchEvent(
                      new CustomEvent("showstar", { detail: id }),
                    );
                  }),
                );
              }
            } else if (item.action === "user_edit") {
              for (const [key, label] of [
                ["user", "Игрок"],
                ["role", "Роль"],
                ["credits", "Кредиты"],
                ["level", "Стаж игрока"],
                ["xp", "Опыт"],
              ])
                UI.row(entry, label, data[key] ?? "—");
            } else if (item.action === "tick")
              UI.row(entry, "Номер фазы", data.tick);
            const full = n("details");
            full.append(
              n("summary", tr("Полные данные")),
              n("pre", JSON.stringify(data, null, 2)),
            );
            entry.append(full);
            rows.append(entry);
          }
          if (!result.items.length) UI.empty(rows, "Записей нет");
          const pager = n("div", undefined, "action-row");
          pager.append(
            UI.button(
              "Назад",
              () => {
                page--;
                return load();
              },
              result.page <= 1,
            ),
            n("span", `${result.page} / ${result.pages}`),
            UI.button(
              "Далее",
              () => {
                page++;
                return load();
              },
              result.page >= result.pages,
            ),
          );
          rows.append(pager);
        } catch (error) {
          notify(error.message, "error");
        }
      };
      search.oninput = () => {
        clearTimeout(timer);
        timer = setTimeout(() => {
          page = 1;
          load();
        }, 250);
      };
      await load();
    },
  });
  window.addEventListener("moduleclose", (event) => {
    if (event.detail === "galaxies") request++;
    if (event.detail === "adminchanges") logRequest++;
  });
  window.addEventListener("signedout", () => {
    request++;
    logRequest++;
    edits.clear();
    draft = null;
    selected = "edem";
  });
}
