import { ledgerAmount } from "../finance/amount.js";
import { paintFlag } from "./flag.js";
import { stationRef } from "../stations/index.js";
import { FormState } from "../../ui/form-state.js";
import { Session, RequestScope } from "../../core/session.js";
import { scopedView } from "../../core/scoped-view.js";
import { I18n } from "../../core/i18n.js";

import { api } from "../../core/api.js";

import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";

import {
  d,
  w,
  res,
  num,
  fmtTime,
  money,
  cargoCapacity,
} from "../../features/selectors.js";
import { reset } from "../../ui/view-state.js";
import { actionAmount } from "../../ui/quantity.js";
import { resourceName } from "../../features/inventory/resource-label.js";
import { can } from "../../features/corporations/permissions.js";

export function createCorporations({ roots, act, command }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    row = UI.row,
    field = (...args) => drafts.field(UI.field, ...args),
    choice = UI.choice;
  let corpTab = "overview";
  let corpScope = "all";
  let selectedCorp = null;
  let requestID = 0;
  let companyList = [];
  let createCorp = false;
  const companyScope = new RequestScope(
    () => Session.signedIn && Modules.isOpen("corporations"),
  );
  function flagPicker(root, initial, change) {
    const host = n("div", undefined, "flag-picker"),
      preview = b(initial, () => {
        const old = host.querySelector(".flag-grid");
        if (old) {
          old.remove();
          return;
        }
        const grid = n("div", undefined, "flag-grid");
        for (const flag of [..."◈◆★☀☾⚑⚙✦"]) {
          const tile = b(flag, () => {
            preview.replaceChildren(n("span", flag, "flag-symbol"));
            preview.value = flag;
            grid.remove();
            change?.(flag);
          });
          tile.classList.toggle("active", flag === preview.value);
          grid.append(tile);
        }
        host.append(grid);
      });
    preview.value = initial;
    preview.className = "flag-preview";
    preview.replaceChildren(n("span", initial, "flag-symbol"));
    host.append(preview);
    root.append(host);
    return preview;
  }
  async function drawCompanies(lease) {
    const key = ++requestID,
      root = roots.corporations;
    const result = await api("companies?scope=" + corpScope);
    if (key !== requestID || !lease.current()) return false;
    companyList = result;
    reset(root);
    const tools = n("div", undefined, "inline-controls");
    choice(
      tools,
      "Показывать",
      [
        ["all", "Все корпорации"],
        ["region", "Корпорации региона"],
        ["system", "Корпорации системы"],
      ],
      corpScope,
      (value) => {
        corpScope = value;
        renderCompanies();
      },
    );
    tools.append(
      b(
        "Создать корпорацию",
        () => {
          createCorp = !createCorp;
          renderCompanies();
        },
        !!Session.data.profile.company_id,
        "Сначала выйдите из текущей корпорации",
      ),
    );
    root.append(tools);
    const layout = n("div", undefined, "master-detail"),
      list = n("section");
    companyList.sort(
      (a, b) =>
        Number(!!b.founder) - Number(!!a.founder) ||
        Number(!!b.starter) - Number(!!a.starter) ||
        a.name.localeCompare(b.name),
    );
    list.append(n("small", tr("Основатели и академия"), "company-group-label"));
    let separated = false;
    for (const c of companyList) {
      if (!c.founder && !c.starter && !separated) {
        list.append(n("hr"), n("small", tr("Корпорации игроков")));
        separated = true;
      }
      const button = b("", () => {
        selectedCorp = c.id;
        createCorp = false;
        renderCompanies();
      });
      button.className = "company-list-row";
      const badge = n("span", c.flag + " " + c.tag, "corp-badge");
      paintFlag(badge, c);
      button.append(
        badge,
        n("span", c.starter || c.founder ? tr(c.name) : c.name),
        n("small", c.founder ? tr("Корпорация-основатель") : num(c.members)),
      );
      button.classList.toggle(
        "selected",
        (selectedCorp || Session.data.profile.company_id) === c.id,
      );
      list.append(button);
    }
    if (!separated)
      list.append(
        n("hr"),
        n("small", tr("Корпорации игроков")),
        n("p", tr("Пока нет корпораций игроков"), "muted"),
      );
    layout.append(list);
    const side = n("aside", undefined, "detail-panel");
    layout.append(side);
    root.append(layout);
    if (createCorp) {
      renderCorpCreate(side);
      return;
    }
    const c =
      companyList.find(
        (c) => c.id === (selectedCorp || Session.data.profile.company_id),
      ) || companyList[0];
    if (!c) return;
    side.append(n("h3", c.starter || c.founder ? tr(c.name) : c.name));
    const my = c.id === Session.data.profile.company_id;
    if (!my) {
      row(side, "Сокращение", c.tag);
      if (!c.founder) row(side, "Участников", c.members);
      if (c.founder) {
        const rep = w().reputation?.find((r) => r.company_id === c.id);
        row(side, "Репутация", rep?.points || 0);
        side.append(
          n("p", tr("Тарифы и услуги доступны в карточках портов."), "muted"),
        );
      } else
        side.append(
          b(
            c.starter ? "Присоединиться" : "Подать заявку",
            () =>
              c.starter
                ? act("companies", { action: "join", id: c.id })
                : command({ action: "apply", id: c.id }),
            !!Session.data.profile.company_id,
            "Сначала выйдите из текущей корпорации",
          ),
        );
      renderInvitations(side);
      return;
    }
    UI.tabs(
      side,
      [
        ["overview", "Обзор"],
        ["members", "Участники"],
        ["finance", "Финансы"],
        ["storage", "Склады"],
        ["projects", "Проекты"],
        ["manage", "Управление"],
      ],
      corpTab,
      (id) => {
        corpTab = id;
        renderCompanies();
      },
    );
    if (corpTab === "overview") {
      if (!c.founder) row(side, "Участников", c.members);
      row(
        side,
        "Владелец",
        w().members?.find((m) => m.id === c.owner)?.username || "—",
      );
      if (can("finance"))
        row(side, "Корпоративный кошелёк", money(w().company_balance));
      side.append(
        b(
          "Выйти из корпорации",
          () =>
            UI.confirm("Выход из корпорации", c.name, () =>
              act("companies", { action: "leave" }),
            ),
          c.owner === Session.data.state.user.id,
          "Сначала передайте владение",
        ),
      );
      renderInvitations(side);
    }
    if (corpTab === "members") {
      side.append(
        UI.table(
          ["Игрок", "Роль", "Действие"],
          (w().members || []).map((m) => {
            const role =
              can("owner") && m.id !== c.owner
                ? UI.picker(
                    [
                      [0, "Участник"],
                      ...(w().roles || []).map((r) => [r.id, r.name]),
                    ],
                    m.company_role || 0,
                    (value) =>
                      command({
                        action: "role_assign",
                        id: m.id,
                        amount: Number(value),
                      }),
                  )
                : n("span", tr(m.role_name));
            return [
              m.username,
              role,
              can("owner") && m.id !== c.owner
                ? b("Передать владение", () =>
                    UI.confirm("Передача владения", m.username, () =>
                      command({ action: "company_transfer", id: m.id }),
                    ),
                  )
                : "—",
            ];
          }),
        ),
      );
      if (can("manage")) {
        const name = field(side, "Игрок: имя или ID", "invite-name", "");
        side.append(
          b("Пригласить", () =>
            command({ action: "invite", name: name.value }),
          ),
        );
        for (const req of (w().requests || []).filter(
          (r) => r.company_id === c.id && r.kind === "application",
        )) {
          const line = n("div", undefined, "inline-controls");
          line.append(
            n("span", req.username),
            b("Принять", () =>
              command({ action: "request_accept", id: req.id }),
            ),
            b("Отклонить", () =>
              command({ action: "request_reject", id: req.id }),
            ),
          );
          side.append(line);
        }
      }
    }
    if (corpTab === "finance") {
      const amount = field(side, "Сумма, CR", "company-amount", 100, "number");
      side.append(
        b("Пополнить", () =>
          command({ action: "company_deposit", amount: Number(amount.value) }),
        ),
      );
      if (can("finance")) {
        side.append(
          b("Снять", () =>
            command({
              action: "company_withdraw",
              amount: Number(amount.value),
            }),
          ),
        );
        row(side, "Баланс", money(w().company_balance));
        side.append(
          UI.table(
            ["Дата", "Участник", "Изменение"],
            (w().company_finance || []).map((f) => [
              fmtTime(f.created_at),
              f.username,
              ledgerAmount(f.amount),
            ]),
          ),
        );
      } else UI.empty(side, "Для просмотра финансов нужны полномочия");
    }
    if (corpTab === "storage") {
      for (const store of w().company_storage || []) {
        const warehouseTitle = n("h3");
        warehouseTitle.append(stationRef(store.star, store.station));
        side.append(warehouseTitle);
        side.append(
          UI.table(
            ["Ресурс", "Количество", "Действие"],
            Object.entries(store.cargo)
              .filter(([, q]) => q > 0)
              .map(([id, q]) => [
                resourceName(id),
                q,
                b(
                  "В трюм",
                  () =>
                    actionAmount(
                      "В трюм",
                      "Количество",
                      Math.min(cargoCapacity(), q),
                      (quantity) =>
                        command({
                          action: "company_cargo_withdraw",
                          resource: id,
                          quantity,
                        }),
                    ),
                  !can("warehouse") ||
                    !d().docked ||
                    d().ship.star !== store.star ||
                    d().ship.object_id !== store.station,
                  "Прибудьте на станцию хранения",
                ),
              ]),
          ),
        );
      }
      if (!w().company_storage?.length)
        UI.empty(side, "На складах пока нет ресурсов");
    }
    if (corpTab === "projects") renderProjects(side);
    if (corpTab === "manage") {
      if (!can("owner")) {
        UI.empty(side, "Управление ролями доступно владельцу");
        return;
      }
      side.append(
        n("h3", tr("Роли") + " · " + (w().roles?.length || 0) + " / 7"),
      );
      const permissionNames = [
        ["finance", "Финансы"],
        ["manage", "Управление участниками"],
        ["projects", "Создание проектов"],
        ["warehouse", "Выдача со склада"],
        ["colonize", "Планетарная колонизация"],
        ["planetary.enter", "Посещение планетарных территорий"],
        ["chat.moderate", "Модерация чата корпорации"],
      ];
      side.append(
        UI.table(
          ["Роль", ...permissionNames.map(([, name]) => name), "Действие"],
          (w().roles || []).map((role) => {
            const values = new Set(role.permissions),
              name = n("input");
            name.value = role.name;
            const boxes = permissionNames.map(([key]) => {
              const box = n("input");
              box.type = "checkbox";
              box.checked = values.has(key);
              box.onchange = () =>
                box.checked ? values.add(key) : values.delete(key);
              return box;
            });
            const controls = n("div", undefined, "inline-controls");
            controls.append(
              b("Сохранить", () =>
                command({
                  action: "role_save",
                  id: role.id,
                  name: name.value,
                  permissions: [...values],
                }),
              ),
              b("Удалить", () =>
                command({ action: "role_delete", id: role.id }),
              ),
            );
            return [name, ...boxes, controls];
          }),
        ),
      );
      const name = field(side, "Название новой роли", "role-name", "");
      side.append(
        b(
          "Добавить роль",
          () =>
            command({ action: "role_save", name: name.value, permissions: [] }),
          w().roles.length >= 7,
          "Максимум 7 настраиваемых ролей",
        ),
        n(
          "p",
          tr("Роли владельца и первоначального участника не входят в лимит."),
          "muted",
        ),
      );
      renderFlagDesign(side, c);
      side.append(
        b("Распустить корпорацию", () =>
          UI.confirm(
            "Роспуск корпорации",
            tr(
              "Остаток средств получит владелец. Активные проекты и склад должны быть закрыты. Построенные порты перейдут корпорации-основателю 01.",
            ),
            () => command({ action: "company_dissolve" }),
          ),
        ),
      );
    }
  }
  function renderInvitations(root) {
    for (const req of (w().requests || []).filter(
      (r) => r.user_id === Session.data.state.user.id,
    )) {
      const line = n("div", undefined, "inline-controls");
      line.append(n("span", tr(req.kind) + " #" + req.company_id));
      if (req.kind === "invitation")
        line.append(
          b(
            "Принять приглашение",
            () => command({ action: "invitation_accept", id: req.id }),
            !!Session.data.profile.company_id,
            "Сначала выйдите из текущей корпорации",
          ),
        );
      line.append(
        b("Отменить", () => command({ action: "request_cancel", id: req.id })),
      );
      root.append(line);
    }
  }
  function renderCorpCreate(root) {
    root.append(n("h3", tr("Создать корпорацию")));
    const form = n("form"),
      head = n("div", undefined, "corp-create-header"),
      preview = flagPicker(head, "◈"),
      fields = n("div");
    head.append(fields);
    const name = field(fields, "Полное название", "company-full-name", ""),
      tag = field(fields, "Сокращение · 3–4 символа", "company-tag", ""),
      color = field(fields, "Цвет", "company-color", "#efad58", "color");
    name.maxLength = 60;
    tag.maxLength = 4;
    const stripe = n("div", undefined, "color-preview");
    color.oninput = () => {
      stripe.style.background = color.value;
      preview.style.color = color.value;
    };
    color.oninput();
    form.append(head, stripe);
    const cost = Session.data.clock.settings.company_cost;
    form.append(
      n("p", money(cost) + " · " + tr("Баланс") + ": " + money(d().credits)),
    );
    const submit = b(
      "Создать корпорацию",
      async () => {
        const ok = await act("companies", {
          action: "create",
          name: name.value,
          tag: tag.value,
          color: color.value,
          flag: preview.value,
        });
        if (ok) {
          createCorp = false;
          selectedCorp = Session.data.profile.company_id;
          await renderCompanies();
        }
      },
      d().credits < cost,
      "Недостаточно кредитов",
    );
    submit.classList.add("primary-action");
    form.append(submit);
    form.onsubmit = (e) => {
      e.preventDefault();
      submit.click();
    };
    root.append(form);
  }
  function renderFlagDesign(root, c) {
    const form = n("section");
    form.append(n("h3", tr("Конструктор флага")));
    const preview = flagPicker(form, c.flag),
      color = field(form, "Цвет корпорации", "flag-color", c.color, "color"),
      bg = field(
        form,
        "Фон флага",
        "flag-background",
        c.flag_design?.background || "#17232d",
        "color",
      ),
      stripe = field(
        form,
        "Полоса",
        "flag-stripe",
        c.flag_design?.stripe || c.color,
        "color",
      ),
      update = () => {
        preview.style.color = color.value;
        preview.style.background = `linear-gradient(135deg,${bg.value} 38%,${stripe.value} 39%,${stripe.value} 53%,${bg.value} 54%)`;
      };
    for (const input of [color, bg, stripe]) input.oninput = update;
    update();
    form.append(
      b("Сохранить флаг", () =>
        command({
          action: "flag_save",
          flag: preview.value,
          color: color.value,
          flag_design: {
            background: bg.value,
            stripe: stripe.value,
            symbol: color.value,
          },
        }),
      ),
    );
    root.append(form);
  }
  function renderProjects(root) {
    for (const p of w().projects || []) {
      const card = n("article", undefined, "project-card");
      card.append(
        n("h3", p.name),
        n("p", tr(p.kind) + " · " + p.star + " · " + tr(p.status)),
      );
      for (const [id, need] of Object.entries(p.required)) {
        const done = p.contributed[id] || 0;
        card.append(resourceName(id), UI.bar(done, need, "Вклад"));
        if (p.status === "active")
          card.append(
            b(
              "Внести со склада",
              () =>
                actionAmount(
                  "Вклад в проект",
                  "Количество",
                  need - done,
                  (quantity) =>
                    command({
                      action: "project_contribute",
                      id: p.id,
                      resource: id,
                      quantity,
                    }),
                ),
              done >= need,
            ),
          );
      }
      const contributions = UI.fold("corporation:contributions:" + p.id);
      contributions.append(n("summary", tr("Вклады участников")));
      for (const item of (w().project_contributions || []).filter(
        (x) => x.project_id === p.id,
      ))
        contributions.append(
          n(
            "p",
            (item.username || "#" + item.user_id) +
              " · " +
              tr(res(item.resource).name) +
              " × " +
              item.quantity,
          ),
        );
      card.append(contributions);
      root.append(card);
    }
    if (can("projects")) {
      const name = field(root, "Название проекта", "project-name", ""),
        type = choice(
          root,
          "Тип проекта",
          [
            ["warehouse", "Корпоративный склад"],
            ["port", "Порт"],
            ["station", "Станция"],
          ],
          "warehouse",
        );
      root.append(
        b(
          "Создать проект",
          () =>
            command({
              action: "project_create",
              name: name.value,
              target: type.value,
            }),
          !d().docked,
          "Стыкуйтесь со станцией",
        ),
      );
    }
  }
  const renderCompanies = scopedView(companyScope, drawCompanies, (error) =>
    notify(error.message, "error"),
  );
  function clear() {
    drafts.clear();
    selectedCorp = null;
    requestID = 0;
    companyScope.invalidate();
    companyList = [];
    createCorp = false;
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  window.addEventListener("chatopenlink", ({ detail }) => {
    if (detail.kind === "company") {
      selectedCorp = Number(detail.id);
      createCorp = false;
      renderCompanies();
    }
  });
  return {
    flagPicker,
    renderCompanies,
    renderInvitations,
    renderCorpCreate,
    renderFlagDesign,
    renderProjects,
    clear,
    close() {
      renderCompanies.invalidate();
    },
  };
}
