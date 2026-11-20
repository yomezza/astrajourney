import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

const settle = (page) =>
  page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );

test("newer user search wins; an old failure cannot overwrite it or show a toast", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  await g.ready();
  await g.open("users");
  await g.page.waitForSelector("#user-search");
  const replies = new Map();
  g.handlers.set("admin/users", async ({ url, json }) => {
    await new Promise((resolve) =>
      replies.set(url.searchParams.get("q"), (body) => {
        resolve();
        return json(body, body.error ? 500 : 200);
      }),
    );
  });
  for (const value of ["alpha", "beta"]) {
    const requested = g.page.waitForRequest(
      (r) => new URL(r.url()).searchParams.get("q") === value,
    );
    await g.page.locator("#user-search").fill(value);
    await g.page.locator("#user-search").press("Enter");
    await requested;
  }
  await replies.get("beta")({
    ...g.data.users,
    items: [{ ...g.data.users.items[0], username: "NEW_RESULT" }],
  });
  await g.page.waitForFunction(() =>
    document.querySelector("#window-users").textContent.includes("NEW_RESULT"),
  );
  await replies.get("alpha")({ error: "OLD_SEARCH_ERROR" });
  await settle(g.page);
  assert.match(
    await g.page.locator("#window-users").textContent(),
    /NEW_RESULT/,
  );
  assert.equal(await g.page.locator("#toast").textContent(), "");
});

test("closing Users invalidates a pending read", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  await g.ready();
  await g.open("users");
  await g.page.waitForSelector("#user-search");
  const previous = await g.page
    .locator("#window-users .module-content")
    .textContent();
  let reply;
  g.handlers.set("admin/users", async ({ json }) => {
    await new Promise(
      (r) =>
        (reply = () => {
          r();
          return json({
            ...g.data.users,
            items: [{ ...g.data.users.items[0], username: "LATE_RESULT" }],
          });
        }),
    );
  });
  const requested = g.page.waitForRequest(
    (r) => new URL(r.url()).pathname === "/api/admin/users",
  );
  await g.page.locator("#user-search").fill("late");
  await g.page.locator("#user-search").press("Enter");
  await requested;
  await g.page.evaluate(() => window.__testApp.Modules.close("users"));
  await reply();
  await settle(g.page);
  assert.equal(
    await g.page.locator("#window-users .module-content").textContent(),
    previous,
  );
});

test("a different account never receives the previous admin's late response", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  await g.ready();
  let reply;
  g.handlers.set("admin/users", async ({ json }) => {
    await new Promise(
      (r) =>
        (reply = () => {
          r();
          return json({ error: "PREVIOUS_ACCOUNT_ERROR" }, 500);
        }),
    );
  });
  const requested = g.page.waitForRequest(
    (r) => new URL(r.url()).pathname === "/api/admin/users",
  );
  await g.open("users");
  await requested;
  await g.page.evaluate(() => window.__testApp.showAuth());
  g.data.state.user = { id: 52, username: "second_pilot", role: "player" };
  await g.login();
  await reply();
  await settle(g.page);
  assert.equal(
    await g.page.locator("#welcome-name").textContent(),
    "second_pilot",
  );
  assert.equal(
    await g.page.locator("#window-users .module-content").textContent(),
    "",
  );
  assert.equal(await g.page.locator("#toast").textContent(), "");
});

test("loss of admin role closes user editors and rejects pending Users results", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  await g.ready();
  await g.open("users");
  await g.page.waitForSelector("#user-search");
  await g.page
    .locator("#window-users")
    .getByRole("button", { name: "Редактировать", exact: true })
    .click();
  await g.page.waitForSelector("#window-user-71[open]");
  g.data.state.user.role = "player";
  await g.refresh();
  assert.equal(await g.page.locator("#window-user-71").count(), 0);
  assert.equal(
    await g.page.locator("#window-users .module-content").textContent(),
    "",
  );
});

test("transient windows release DOM nodes and global resize listeners", async (t) => {
  const g = await game(t);
  await g.ready();
  const cdp = await g.context.newCDPSession(g.page);
  const count = async () => {
    const obj = await cdp.send("Runtime.evaluate", { expression: "window" });
    const result = await cdp.send("DOMDebugger.getEventListeners", {
      objectId: obj.result.objectId,
    });
    return result.listeners.filter((l) => l.type === "resize").length;
  };
  const windows = await g.page.locator(".module-window").count(),
    listeners = await count();
  await g.page.evaluate(() => {
    for (let i = 0; i < 60; i++) {
      const id = "lifecycle-" + i;
      window.__testApp.Modules.create(id, "Test", 400, 300, {
        transient: true,
      });
      window.__testApp.Modules.open(id);
      window.__testApp.Modules.close(id);
    }
  });
  await settle(g.page);
  assert.equal(await g.page.locator(".module-window").count(), windows);
  assert.equal(await count(), listeners);
});

test("dirty temporary editors survive close but are cleared on logout", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.evaluate(() => {
    const root = window.__testApp.Modules.create(
      "dirty-test",
      "Test",
      400,
      300,
      {
        transient: true,
      },
    );
    const form = document.createElement("form");
    form.dataset.dirty = "true";
    form.textContent = "DRAFT";
    root.append(form);
    window.__testApp.Modules.open("dirty-test");
    window.__testApp.Modules.close("dirty-test");
  });
  assert.equal(await g.page.locator("#window-dirty-test").count(), 1);
  await g.page.evaluate(() => window.__testApp.showAuth());
  assert.equal(await g.page.locator("#window-dirty-test").count(), 0);
  assert.equal(await g.page.locator("#welcome-name").textContent(), "");
  assert.equal(await g.page.locator("#credits").textContent(), "");
});

test("picker uses a modal's top layer and reports asynchronous callback errors", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.evaluate(() => {
    const modal = document.createElement("dialog");
    modal.id = "test-modal";
    document.body.append(modal);
    const picker = window.__testApp.UI.picker(
      [
        [1, "one"],
        [2, "two"],
      ],
      1,
      async () => {
        throw Error("EXPECTED_ACTION_ERROR");
      },
    );
    picker.id = "modal-picker";
    modal.append(picker);
    modal.showModal();
  });
  await g.page.locator("#modal-picker").click();
  assert.equal(
    await g.page
      .locator(".picker-popover")
      .evaluate((e) => e.closest("dialog")?.id),
    "test-modal",
  );
  await g.page.getByRole("option", { name: "two", exact: true }).click();
  await g.page.waitForFunction(() =>
    document
      .getElementById("toast")
      .textContent.includes("EXPECTED_ACTION_ERROR"),
  );
});

test("partial snapshot failure leaves the previous accepted balance intact", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  const before = await g.page.locator("#credits").textContent();
  g.data.expedition.credits += 1000;
  g.handlers.set("state", ({ json }) => json({ error: "temporary" }, 503));
  await g.refresh();
  assert.equal(await g.page.locator("#credits").textContent(), before);
  g.handlers.delete("state");
  await g.refresh();
  assert.match(await g.page.locator("#balance-change").textContent(), /^\+/);
});

test("poll refresh preserves ship rename drafts without executing an action", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("ship");
  await g.page.waitForSelector("#ship-rename");
  await g.page.locator("#ship-rename").fill("Unsaved name");
  await g.page.locator("#profile-menu > summary").click();
  await g.refresh();
  assert.equal(
    await g.page.locator("#ship-rename").inputValue(),
    "Unsaved name",
  );
  assert.equal(
    g.requests.some((r) => r.method === "POST"),
    false,
  );
});
