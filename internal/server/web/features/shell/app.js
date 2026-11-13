import { bindAction } from "../../ui/actions.js";
import { paintFlag } from "../corporations/flag.js";
import { personalizeStations } from "../stations/index.js";
import { Format } from "../../core/format.js";
import { Session, invalidateSession } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { refresh, configureSnapshot } from "../../core/snapshot-store.js";
import { notify, configureFeedback } from "../../ui/feedback.js";
import { Modules } from "../../ui/runtime.js";
import { world } from "../../core/world-store.js";

import { Startup } from "../../ui/startup.js";

export function initApp() {
  const $ = (id) => document.getElementById(id),
    t = (s) => I18n.t(s);
  let starting = true,
    pending = false,
    mode = "login",
    balanceOwner = null,
    previousBalance = null,
    balanceTimer;

  const authStartup = Startup.acquire(
    "auth",
    "Подключение к серверу",
    "Проверяем сессию и загружаем данные пилота…",
    100,
  );
  function startupStatus(failed = false) {
    authStartup.update(
      failed ? "Не удалось подключиться" : "Подключение к серверу",
      failed
        ? "Сервер пока не отвечает. Проверьте соединение и повторите попытку."
        : "Проверяем сессию и загружаем данные пилота…",
    );
    if (failed)
      authStartup.fail(
        "Сервер пока не отвечает. Проверьте соединение и повторите попытку.",
        () => {
          if (!starting || pending) return;
          startupStatus();
          refresh(true);
        },
      );
  }
  function finishStartup() {
    starting = false;
    authStartup.release();
  }
  function authMode(value) {
    mode = value;
    $("login-tab").classList.toggle("active", mode === "login");
    $("register-tab").classList.toggle("active", mode === "register");
    $("submit").textContent = t(
      mode === "login" ? "Войти в терминал →" : "Создать аккаунт →",
    );
    $("auth-form").elements.password.minLength = mode === "login" ? 1 : 1;
    $("auth-form").elements.password.autocomplete =
      mode === "login" ? "current-password" : "new-password";
    $("password-hint").textContent = t(
      mode === "login"
        ? "Используйте данные вашей компании."
        : "Имя: латиница, цифры, _ и -.",
    );
  }
  let authSubmitting = false;
  function showAuth() {
    balanceOwner = null;
    previousBalance = null;
    clearTimeout(balanceTimer);
    $("balance-change")?.replaceChildren();
    invalidateSession();
    pending = false;
    for (const menu of document.querySelectorAll(".dropdown"))
      menu.open = false;
    Modules.closeAll();
    Modules.clearPrivate();
    for (const id of [
      "profile-nick",
      "welcome-name",
      "credits",
      "location",
      "overview-region",
      "overview-company",
      "overview-level",
      "overview-xp-text",
      "star-date",
      "connection",
    ])
      $(id)?.replaceChildren();
    $("overview-upgrade").disabled = true;
    $("overview-xp").value = 0;

    window.dispatchEvent(new Event("signedout"));
    for (const id of [
      "game",
      "sidebar",
      "profile-menu",
      "admin-menu",
      "notifications-button",
    ])
      $(id).hidden = true;
    $("auth").hidden = false;
    finishStartup();
  }
  function renderSnapshot() {
    const { state, profile, expedition } = Session.data;
    $("auth").hidden = true;
    for (const id of [
      "game",
      "sidebar",
      "profile-menu",
      "notifications-button",
    ])
      $(id).hidden = false;
    $("admin-menu").hidden = state.user.role !== "admin";
    if (state.user.role !== "admin") Modules.closeAudience("admin");
    $("profile-nick").textContent = state.user.username;
    $("welcome-name").textContent = state.user.username;
    const company = $("overview-company");
    company.className = profile.company_tag ? "corp-badge" : "muted";
    company.style.color = profile.company_color || "";
    company.style.background = "";
    company.textContent = profile.company_tag
      ? `${profile.company_flag} [${profile.company_tag}]`
      : t("Без компании");
    if (profile.company_tag)
      paintFlag(company, {
        tag: profile.company_tag,
        color: profile.company_color,
        flag: profile.company_flag,
        flag_design: profile.company_flag_design,
      });
    company.dataset.helpRaw =
      profile.company_id === 1
        ? t(profile.company)
        : profile.company || t("Без компании");
    company.dataset.help = company.dataset.helpRaw;
    company.tabIndex = 0;
    const balance = Number(expedition.credits),
      indicator = $("balance-change");
    if (
      balanceOwner === state.user.id &&
      previousBalance !== null &&
      balance !== previousBalance
    ) {
      const delta = balance - previousBalance;
      clearTimeout(balanceTimer);
      const text = document.createElement("span");
      text.className = "balance-delta " + (delta > 0 ? "income" : "expense");
      text.textContent =
        (delta > 0 ? "+" : "-") + Format.quantity(Math.abs(delta)) + " CR";
      indicator.replaceChildren(text);
      balanceTimer = setTimeout(() => indicator.replaceChildren(), 3200);
    } else if (balanceOwner !== state.user.id) {
      clearTimeout(balanceTimer);
      indicator.replaceChildren();
    }
    balanceOwner = state.user.id;
    previousBalance = balance;
    $("credits").textContent = Format.number(balance);
    $("star-date").textContent = state.date.replace(
      "ЭД",
      I18n.language === "en" ? "ED" : "ЭД",
    );
    $("location").textContent = expedition.ship.destination
      ? expedition.system.name +
        " → " +
        (world.data?.stars[expedition.ship.destination - 1]?.name ||
          String(expedition.ship.destination))
      : expedition.system.name;
    $("overview-region").textContent = t(expedition.region);
    $("overview-level").textContent = profile.level;
    $("overview-xp").max = profile.required_xp;
    $("overview-xp").value = profile.xp;
    $("overview-xp").dataset.help =
      "Опыт даётся за разведку (+10) и завершённый цикл добычи (+5). Стаж принадлежит игроку, не кораблю.";
    $("overview-xp-text").textContent =
      `${profile.xp} / ${profile.required_xp} XP`;
    $("overview-upgrade").disabled =
      profile.xp < profile.required_xp ||
      expedition.credits < profile.upgrade_cost ||
      profile.level >= 1000000;
    $("overview-upgrade").dataset.help = `${profile.upgrade_cost} CR`;
    $("connection").textContent =
      t("Данные обновлены: ") + Format.time(Date.now());
    $("server-status").textContent = t("● СЕРВЕР ОНЛАЙН");
    window.dispatchEvent(new Event("gamedata"));
    finishStartup();
  }
  configureSnapshot((data) => personalizeStations(data));
  configureFeedback((text, level) =>
    Session.signedIn
      ? api("notifications", { action: "add", message: text, level }).then(() =>
          window.dispatchEvent(new Event("notificationchange")),
        )
      : undefined,
  );
  window.addEventListener("snapshotaccepted", renderSnapshot);
  window.addEventListener("connectionerror", ({ detail: error }) => {
    if (error.status === 401) showAuth();
    else {
      $("server-status").textContent = t("Переподключение…");
      if (starting) startupStatus(true);
    }
  });
  window.addEventListener("sessionexpired", ({ detail }) => {
    if (detail === Session.epoch) showAuth();
  });
  $("login-tab").onclick = () => authMode("login");
  $("register-tab").onclick = () => authMode("register");
  $("auth-form").onsubmit = async (e) => {
    e.preventDefault();
    if (authSubmitting) return;
    authSubmitting = true;
    Session.epoch++;
    pending = false;
    $("submit").disabled = true;
    $("submit").setAttribute("aria-busy", "true");
    $("auth-error").textContent = "";
    try {
      await api(mode, {
        username: e.target.elements.username.value,
        password: e.target.elements.password.value,
      });
      e.target.elements.password.value = "";
      await refresh(true);
    } catch (error) {
      $("auth-error").textContent = error.message;
      for (const input of e.target.querySelectorAll("input")) {
        input.setAttribute("aria-invalid", "true");
        input.setAttribute("aria-describedby", "auth-error");
        input.oninput = () => input.removeAttribute("aria-invalid");
      }
    } finally {
      authSubmitting = false;
      $("submit").disabled = false;
      $("submit").removeAttribute("aria-busy");
    }
  };
  $("logout").onclick = async () => {
    Session.epoch++;
    pending = false;
    try {
      await api("logout", {});
      showAuth();
    } catch (e) {
      notify(e.message, "error");
    }
  };
  bindAction($("admin-tick"), async () => {
    const epoch = Session.epoch;
    await api("admin/tick", {});
    if (epoch !== Session.epoch) return;
    await refresh(true);
    if (Session.signedIn && Session.data.state?.user.role === "admin")
      notify("Тик выполнен");
  });
  bindAction($("overview-upgrade"), async () => {
    const epoch = Session.epoch;
    await api("profile/upgrade", {});
    if (epoch !== Session.epoch) return;
    await refresh(true);
    if (epoch === Session.epoch && Session.signedIn) notify("Стаж повышен");
  });
  window.addEventListener("languagechange", () => {
    authMode(mode);
    if (Session.signedIn) refresh();
  });
  I18n.ready.then(() => {
    I18n.apply();
    authMode("login");
    refresh();
  });
  setInterval(() => {
    if (
      Session.signedIn &&
      Date.now() - Session.lastPoll >=
        Math.min(
          5000,
          Math.max(1000, (Session.data.state?.tick_seconds || 10) * 1000),
        )
    )
      refresh();
  }, 1000);

  return { showAuth };
}
