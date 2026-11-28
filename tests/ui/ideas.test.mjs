import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("idea status has only a colored border and admin can save a review", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  const items = ["pending", "approved", "rejected"].map((status, i) => ({
    id: i + 1,
    status,
    title: "Idea " + i,
    description: "A proposed improvement",
    category: "other",
    author: "test_pilot",
    created_at: new Date().toISOString(),
  }));
  g.handlers.set("ideas", ({ json }) =>
    json({ items, page: 1, pages: 1, total: items.length }),
  );
  g.handlers.set("admin/ideas", ({ req, json }) => {
    const b = req.postDataJSON();
    items.find((i) => i.id === b.id).status = b.status;
    return json({ id: b.id, status: b.status });
  });
  await g.ready();
  await g.open("ideas");
  await g.page.getByRole("tab", { name: "Мои идеи" }).click();
  await g.page.locator("#window-ideas .idea-list-row .idea-status").first().waitFor();
  const styles = await g.page
    .locator("#window-ideas .idea-list-row .idea-status")
    .evaluateAll((tags) =>
      tags.map((e) => {
        const s = getComputedStyle(e);
        return {
          status: e.dataset.status,
          color: s.color,
          border: s.borderTopColor,
          background: s.backgroundColor,
          align: s.textAlign,
        };
      }),
    );
  assert.equal(styles.length, 3);
  assert.equal(new Set(styles.map((s) => s.border)).size, 3);
  assert.equal(new Set(styles.map((s) => s.color)).size, 1);
  for (const s of styles) {
    assert.equal(s.background, "rgba(0, 0, 0, 0)");
    assert.equal(s.align, "center");
  }
  await g.page.evaluate(() => window.__testApp.Modules.close("ideas"));
  await g.open("adminideas");
  await g.page.locator('#window-adminideas [data-field="Статус идеи"]').click();
  await g.page.getByRole("option", { name: "Одобрено", exact: true }).click();
  await g.page
    .getByRole("button", { name: "Сохранить статус", exact: true })
    .click();
  await g.page.waitForFunction(
    () =>
      document.querySelector("#window-adminideas .idea-detail .idea-status")
        ?.dataset.status === "approved",
  );
  await g.page.evaluate(() => window.__testApp.Modules.close("adminideas"));
  await g.open("ideas");
  await g.page.waitForFunction(
    () =>
      [...document.querySelectorAll("#window-ideas .idea-list-row .idea-status")]
      .filter((tag) => tag.dataset.status === "approved").length === 2,
  );
  assert.equal(
    g.requests.filter((r) => r.method === "POST" && r.path === "admin/ideas")
      .length,
    1,
  );
});

const sample = (id, properties = {}) => ({
  id,
  title: "Предложение " + id,
  description: "Улучшение поможет быстрее ориентироваться в терминале и выполнять задачи.",
  author: "navigator",
  category: "interface",
  status: "approved",
  created_at: new Date(Date.UTC(2026, 10, id)).toISOString(),
  published_at: new Date(Date.UTC(2026, 10, id + 2)).toISOString(),
  ...properties,
});
const response = (items, page = 1, pages = 1, total = items.length) => ({
  items, page, pages, total,
});

test("catalog uses server search, filters and pagination and ignores stale responses", async (t) => {
  const g = await game(t, { signedIn: true });
  const root = g.page.locator("#window-ideas");
  let releaseOld;
  const old = new Promise((resolve) => { releaseOld = resolve; });
  t.after(releaseOld);
  g.handlers.set("ideas", async ({ url, json }) => {
    const q = url.searchParams;
    if (q.get("scope") === "mine") return json(response([]));
    if (q.get("search") === "старый") {
      await old;
      return json(response([sample(1, { title: "Старый ответ" })]));
    }
    if (q.get("search") === "несуществующий") return json(response([]));
    if (q.get("category") === "interface")
      return json(response([sample(13, { title: "Найденная идея" })]));
    if (q.get("page") === "2") return json(response([sample(12)], 2, 2, 11));
    return json(response(Array.from({ length: 10 }, (_, i) => sample(i + 1)), 1, 2, 11));
  });
  await g.ready();
  await g.open("ideas");
  await root.getByRole("button", { name: "Следующая страница" }).click();
  await root.getByRole("heading", { name: "Предложение 12" }).waitFor();
  assert.equal(await root.locator(".pagination span").textContent(), "2 / 2");
  const search = root.getByRole("searchbox");
  await search.fill("старый");
  await g.page.waitForRequest((r) => new URL(r.url()).searchParams.get("search") === "старый");
  await search.fill("новый");
  await root.getByLabel("Категория").selectOption("interface");
  await root.getByLabel("Сортировка").selectOption("oldest");
  await root.getByRole("heading", { name: "Найденная идея" }).waitFor();
  releaseOld();
  const query = new URLSearchParams(g.requests.filter((r) => r.path === "ideas").at(-1).query);
  assert.deepEqual(Object.fromEntries(query), {
    scope: "community", page: "1", sort: "oldest", search: "новый", category: "interface",
  });
  await root.getByRole("tab", { name: "Мои идеи" }).click();
  await root.getByText("У вас пока нет предложений").waitFor();
  await root.getByRole("tab", { name: "Идеи сообщества" }).click();
  assert.equal(await search.inputValue(), "новый");
  assert.equal(await root.getByLabel("Категория").inputValue(), "interface");
  assert.equal(await root.getByLabel("Сортировка").inputValue(), "oldest");
  await root.getByRole("heading", { name: "Найденная идея" }).waitFor();
  assert.equal(await root.getByText("Старый ответ").count(), 0);
  await search.fill("несуществующий");
  await root.getByText("По вашему запросу ничего не найдено").waitFor();
});

test("draft, successful submission, retry and old account data are scoped to the session", async (t) => {
  const g = await game(t, { signedIn: true });
  const root = g.page.locator("#window-ideas");
  let stage = "network",
    own = null;
  let releaseHistory;
  const history = new Promise((resolve) => { releaseHistory = resolve; });
  t.after(releaseHistory);
  g.handlers.set("ideas", async ({ req, url, route, json }) => {
    if (req.method() === "POST") {
      const body = req.postDataJSON();
      own = sample(20, { ...body, status: "pending" });
      return json({ id: own.id, saved: true }, 201);
    }
    if (url.searchParams.get("scope") === "mine") {
      if (stage === "pending") {
        await history;
        return json(response([sample(21, { title: "Старая приватная идея" })]));
      }
      return json(response(own ? [own] : []));
    }
    if (stage === "network") return route.abort("internetdisconnected");
    if (stage === "server") return json({ error: "Ошибка" }, 500);
    return json(response(own ? [own] : []));
  });
  await g.ready();
  await g.open("ideas");
  await root.getByText("Нет соединения с сервером").waitFor();
  stage = "server";
  await root.getByRole("button", { name: "Повторить загрузку" }).click();
  await root.getByText("Не удалось загрузить идеи").waitFor();
  stage = "ready";
  await root.getByRole("button", { name: "Повторить загрузку" }).click();
  await root.getByText("Идей сообщества пока нет").waitFor();
  const formTab = root.getByRole("tab", { name: "Предложить идею" });
  const submit = root.getByRole("button", { name: "Отправить идею", exact: true });
  await formTab.click();
  assert.ok(await submit.isDisabled());
  await root.getByLabel("Название идеи").fill("Предложение из другой сессии");
  await root.locator("#idea-description").fill("Коротко");
  assert.ok(await submit.isDisabled());
  const description = "Первый абзац.\nВторой абзац. 🚀";
  await root.locator("#idea-description").fill(description);
  assert.equal(await root.locator(".idea-counter").textContent(), [...description].length + " / 3000");
  await root.getByRole("tab", { name: "Идеи сообщества" }).click();
  await formTab.click();
  assert.equal(await root.locator("#idea-description").inputValue(), description);
  await submit.click();
  await root.getByRole("button", { name: "Открыть мою идею" }).click();
  await root.locator(".idea-detail").getByText("На рассмотрении").waitFor();
  assert.equal(await root.locator(".idea-detail .idea-description").textContent(), description);
  await root.getByRole("tab", { name: "Идеи сообщества" }).click();
  const pendingRow = root.locator(".idea-list-row").filter({ hasText: "Предложение из другой сессии" });
  await pendingRow.getByText("На рассмотрении").waitFor();
  assert.equal(await pendingRow.locator("time").getAttribute("title"), "Дата создания");

  await formTab.click();
  await root.getByLabel("Название идеи").fill("Приватный черновик");
  stage = "pending";
  await Promise.all([
    g.page.waitForRequest((r) => new URL(r.url()).searchParams.get("scope") === "mine"),
    root.getByRole("tab", { name: "Мои идеи" }).click(),
  ]);
  await g.page.evaluate(() => window.__testApp.showAuth());
  releaseHistory();
  stage = "ready";
  own = null;
  await g.login();
  await g.open("ideas");
  await root.getByText("Идей сообщества пока нет").waitFor();
  await root.getByRole("tab", { name: "Мои идеи" }).click();
  await root.getByText("У вас пока нет предложений").waitFor();
  assert.doesNotMatch(await root.textContent(), /Старая приватная идея|Предложение из другой сессии/);
  await formTab.click();
  assert.equal(await root.getByLabel("Название идеи").inputValue(), "");
  assert.equal(await root.getByRole("button", { name: "Открыть мою идею" }).count(), 0);
});

test("responsive catalog preserves mobile list state and renders long ideas as text", async (t) => {
  const g = await game(t, { signedIn: true });
  const shotDir = process.env.IDEAS_SCREENSHOTS;
  if (shotDir) await mkdir(shotDir, { recursive: true });
  const descriptions = [
    "Можно видеть состояние связи, параметры корабля и часы в одной компактной строке. Настраиваемая панель позволит выбрать нужные показатели.",
    "Сохранять набор открытых окон для отдельных задач, чтобы быстро возвращаться к разведке, торговле и исследованиям.\n\nДля каждого профиля можно запоминать положение карты, размер журнала и выбранные фильтры. Это пригодится пилотам, которые работают с несколькими кораблями и часто переключаются между маршрутами.",
    "Первый абзац.\n<img src=x onerror='alert(1)'> & <script>alert(2)</script>\n" + "К".repeat(1900),
  ];
  const titles = [
    "Компактная строка состояния", "Профили окон терминала", "Журнал сигналов экспедиции",
    "Быстрые заметки на карте", "Сравнение цен на станциях", "Подсказки к маршрутам",
    "Отметки изученных систем", "Поиск открытых окон", "Уведомление о прибытии", "Список задач станции",
  ];
  const items = titles.map((title, i) => sample(10 - i, {
    title, description: i === 9 ? descriptions[2] : descriptions[i % 2],
    author: ["orbital_pilot", "vega", "navigator"][i % 3],
  }));
  items[0].published_at = null;
  g.handlers.set("ideas", ({ json }) => json(response(items)));
  await g.ready();
  await g.open("ideas");
  const root = g.page.locator("#window-ideas"),
    rows = root.locator(".idea-list-row"),
    detail = root.locator(".idea-detail"),
    entries = root.locator(".idea-entries");
  await rows.first().waitFor();
  await rows.first().getByText("Одобрено").waitFor();
  assert.match(await rows.first().locator("time").textContent(), /^Создано /);
  assert.equal(await rows.nth(1).locator("time").getAttribute("title"), "Дата публикации");
  const originalURL = g.page.url();
  for (const width of [1440, 768, 390, 320]) {
    await g.page.setViewportSize({ width, height: width < 500 ? 820 : 950 });
    const layout = await root.locator(".ideas-catalog").evaluate((e) => ({
      narrow: e.getBoundingClientRect().width <= 620,
      viewport: window.innerWidth,
    }));
    if (width === 1440) assert.equal(layout.narrow, false);
    if (width < 500) assert.equal(layout.narrow, true);
    assert.equal(layout.viewport, width);
    const back = detail.getByRole("button", { name: "К списку идей" });
    if (await back.isVisible()) await back.click();
    if (shotDir && width === 390) {
      await entries.evaluate((e) => { e.scrollTop = 0; });
      await root.screenshot({ path: join(shotDir, "390-list.png") });
    }
    const row = rows.nth(9);
    if (layout.narrow) assert.equal(await detail.isVisible(), false);
    await row.scrollIntoViewIfNeeded();
    const scrollTop = await entries.evaluate((e) => e.scrollTop);
    await row.focus();
    await row.press("Enter");
    await detail.getByRole("heading", { name: titles[9] }).waitFor();
    assert.equal(await detail.locator(".idea-description").textContent(), descriptions[2]);
    assert.equal(await detail.locator("img,script").count(), 0);
    assert.equal(await detail.locator(".idea-description").evaluate((e) => getComputedStyle(e).whiteSpace), "pre-wrap");
    assert.equal(await root.evaluate((e) => e.scrollWidth <= e.clientWidth + 1), true);
    assert.equal(g.page.url(), originalURL);
    if (layout.narrow) {
      assert.equal(await rows.first().isVisible(), false);
      await back.click();
      await rows.first().waitFor();
      assert.equal(await entries.evaluate((e) => e.scrollTop), scrollTop);
      assert.ok(await row.evaluate((e) => e === document.activeElement));
    } else {
      assert.equal(await rows.first().isVisible(), true);
      assert.equal(await row.getAttribute("aria-pressed"), "true");
    }
    if (shotDir) {
      await rows.nth(1).click();
      await detail.getByRole("heading", { name: titles[1] }).waitFor();
      await entries.evaluate((e) => { if (e.clientHeight) e.scrollTop = 0; });
      await root.screenshot({ path: join(shotDir, width + "-detail.png") });
    }
  }
});

test("legacy language preference is migrated and the Russian dictionary remains active", async (t) => {
  const g = await game(t, { storage: { language: "en" } });
  await g.ready();
  await g.open("settings");
  assert.equal(await g.page.locator("#window-settings #language").count(), 0);
  assert.deepEqual(await g.page.evaluate(() => {
    const { I18n } = window.__testApp;
    I18n.set("en");
    return {
      language: I18n.language,
      documentLanguage: document.documentElement.lang,
      stored: localStorage.getItem("language"),
      translation: I18n.t("Идей: {total}", { total: 3 }),
    };
  }), { language: "ru", documentLanguage: "ru", stored: "ru", translation: "Идей: 3" });
  assert.ok(g.assets.includes("/ru.json"));
  assert.ok(!g.assets.includes("/en.json"));
});
