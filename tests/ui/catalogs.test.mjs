import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("colony metadata ignores obsolete responses and retries after research changes without losing selection/search", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.page.evaluate(async () => {
    const { mountColonyCatalog, clearColonyCatalog } = await import(
      "/features/planet/colony-catalog.js"
    );
    const root = document.createElement("div");
    document.body.append(root);
    const pending = [],
      data = {
        projects: [
          { id: 101, star: 2, planet: 7, name: "Colony", status: "funding" },
        ],
      };
    const options = {
      language: () => "ru",
      assetBase: "/",
      starName: () => "System",
      resourceName: (x) => x,
      onVisit() {},
      onProject() {},
      onSurface() {},
      loadSystem: () => new Promise((resolve) => pending.push(resolve)),
    };
    const render = (revision) =>
      mountColonyCatalog(root, data, { ...options, revision });
    render(1);
    const search = root.querySelector("input");
    search.value = "Colony";
    search.dispatchEvent(new Event("input"));
    render(2); // Research changed while the previous HTTP response was pending.
    window.__catalog = { root, pending, render, clearColonyCatalog };
    pending[0]({
      star: { name: "Stale system" },
      sites: [{ planet: 7, name: "Stale planet", kind: "Ледяная" }],
    });
  });
  await g.page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(resolve)),
  );
  assert.equal(
    await g.page.evaluate(() => {
      const c = window.__catalog;
      c.render(2);
      return c.pending.length;
    }),
    2,
    "an obsolete request must not clear the new request's in-flight marker",
  );
  await g.page.evaluate(() =>
    window.__catalog.pending[1]({
      star: { name: "Current system" },
      sites: [{ planet: 7, name: "Current planet", kind: "Садовый мир" }],
    }),
  );
  await g.page.waitForFunction(
    () =>
      window.__catalog.root.querySelector(".colony-catalog-planet-name")
        ?.textContent === "Current planet",
  );
  const result = await g.page.evaluate(() => {
    const root = window.__catalog.root;
    return {
      src: root.querySelector("img").getAttribute("src"),
      query: root.querySelector("input").value,
      selected: root.querySelector('[aria-pressed="true"]').dataset.projectId,
      text: root.textContent,
    };
  });
  assert.equal(result.src, "/object-garden.svg");
  assert.equal(result.query, "Colony");
  assert.equal(result.selected, "101");
  assert.ok(!result.text.includes("Stale"));
  // A late response after sign-out/disposal cannot restore private catalogue data.
  await g.page.evaluate(() => {
    const c = window.__catalog;
    c.render(3);
    c.clearColonyCatalog(c.root);
    c.root.replaceChildren();
    c.pending[2]({
      star: { name: "Private system" },
      sites: [{ planet: 7, name: "Private planet", kind: "Ледяная" }],
    });
  });
  await g.page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(resolve)),
  );
  assert.equal(
    await g.page.evaluate(() => window.__catalog.root.textContent),
    "",
  );
});
