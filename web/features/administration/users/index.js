import { pager } from "../../../ui/pager.js";

import { FormState } from "../../../ui/form-state.js";
import { Session, RequestScope } from "../../../core/session.js";
import { scopedView } from "../../../core/scoped-view.js";
import { I18n } from "../../../core/i18n.js";

import { api } from "../../../core/api.js";

import { UI, Modules } from "../../../ui/runtime.js";
import { notify } from "../../../ui/feedback.js";
import { AdminResources } from "../../../features/runtime.js";

import { money } from "../../../features/selectors.js";
import { reset } from "../../../ui/view-state.js";

export function createAdministrationUsers({ roots, act, command }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    field = (...args) => drafts.field(UI.field, ...args),
    choice = UI.choice;
  let userPage = 1;
  let userQuery = "",
    searchTimer;
  const usersScope = new RequestScope(
    () =>
      Session.signedIn &&
      Session.data.state?.user.role === "admin" &&
      Modules.isOpen("users"),
  );
  async function drawUsers(lease) {
    const result = await api(
        "admin/users?page=" + userPage + "&q=" + encodeURIComponent(userQuery),
      ),
      root = roots.users,
      focused = document.activeElement?.id === "user-search",
      caret = document.activeElement?.selectionStart;
    if (!lease.current()) return false;
    reset(root);
    userPage = result.page;
    const search = field(
      root,
      "Поиск по никнейму или ID",
      "user-search",
      userQuery,
      "search",
    );

    search.oninput = () => {
      userQuery = search.value;
      clearTimeout(searchTimer);
      usersScope.invalidate();
      searchTimer = setTimeout(() => {
        userPage = 1;
        renderUsers();
      }, 250);
    };
    search.onkeydown = (e) => {
      if (e.key === "Enter") {
        clearTimeout(searchTimer);
        usersScope.invalidate();
        userQuery = search.value;
        userPage = 1;
        renderUsers();
      }
    };
    const paging = pager({
      page: userPage,
      pages: result.pages,
      total: result.total,
      className: "pagination sticky-toolbar",
      change: (value) => {
        userPage = value;
        return renderUsers();
      },
    });
    root.append(
      paging,
      UI.table(
        ["ID", "Игрок", "Роль", "Баланс", "Стаж игрока", "Действие"],
        result.items.map((u) => [
          u.id,
          u.username,
          tr(u.role) + (u.bot ? " · BOT" : ""),
          money(u.credits),
          u.level,
          b("Редактировать", () => editUser(u)),
        ]),
      ),
    );
    if (focused) {
      search.focus();
      search.setSelectionRange(caret, caret);
    }
  }
  function editUser(u) {
    const id = "user-" + u.id,
      root = Modules.create(id, "Редактирование пользователя", 550, 640, {
        transient: true,
        audience: "admin",
      });
    reset(root);
    const form = n("form");
    form.append(n("h3", tr("Основные сведения")));
    const name = field(form, "Позывной", "edit-name-" + u.id, u.username);
    name.disabled = u.bot;
    form.append(n("h3", tr("Игровые показатели")));
    const values = n("div", undefined, "settings-fields"),
      credits = field(
        values,
        "Кредиты",
        "edit-credits-" + u.id,
        u.credits,
        "number",
      ),
      level = field(
        values,
        "Стаж игрока",
        "edit-level-" + u.id,
        u.level,
        "number",
      ),
      xp = field(values, "Опыт", "edit-xp-" + u.id, u.xp, "number");
    credits.min = 0;
    credits.max = Number.MAX_SAFE_INTEGER;
    credits.step = 1;
    level.min = 1;
    level.max = 1000000;
    xp.min = 0;
    xp.max = 1000000000;
    form.append(
      values,
      b("Выдача ресурсов", () => AdminResources.open(u.id)),
      n("h3", tr("Доступ")),
    );
    const role = choice(
      form,
      "Роль",
      [
        ["player", "ИГРОК"],
        ["admin", "АДМИНИСТРАТОР"],
      ],
      u.role,
    );
    role.disabled = u.bot;
    const pass = field(
      form,
      "Новый пароль · пусто — без изменений",
      "edit-password-" + u.id,
      "",
      "password",
    );
    pass.disabled = u.bot;
    pass.autocomplete = "new-password";
    const dirty = UI.dirty(
      form,
      async () => {
        const ok = await act("admin/user", {
          id: u.id,
          username: name.value,
          credits: Number(credits.value),
          level: Number(level.value),
          xp: Number(xp.value),
          role: role.value,
          password: pass.value,
        });
        if (
          ok &&
          Session.data.state?.user.role === "admin" &&
          root.isConnected
        ) {
          Object.assign(u, {
            username: name.value,
            credits: Number(credits.value),
            level: Number(level.value),
            xp: Number(xp.value),
            role: role.value,
          });
          pass.value = "";
          renderUsers();
          editUser(u);
        }
        return ok;
      },
      () => {
        drafts.forget(
          ...[name, credits, level, xp, pass].map((input) => input.id),
        );
        editUser(u);
      },
    );
    role.onchange = dirty;
    form.addEventListener("input", (e) =>
      e.target.classList.add("field-dirty"),
    );
    root.append(form);
    Modules.open(id);
  }
  const renderUsers = scopedView(usersScope, drawUsers, (error) =>
    notify(error.message, "error"),
  );
  function clear() {
    clearTimeout(searchTimer);
    drafts.clear();
    userPage = 1;
    userQuery = "";
    usersScope.invalidate();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return {
    renderUsers,
    editUser,
    clear,
    close() {
      clearTimeout(searchTimer);
      renderUsers.invalidate();
    },
  };
}
