import assert from "node:assert/strict";
import { test } from "node:test";
import {
  Session,
  RequestScope,
} from "../../web/core/session.js";
import { Preferences } from "../../web/core/preferences.js";
import { request } from "../../web/core/api-client.js";
import { AssetCache } from "../../web/core/asset-cache.js";
import { I18n } from "../../web/core/i18n.js";
import { Format } from "../../web/core/format.js";

test("scope invalidates old generations, aborts reads and rejects another session", () => {
  Session.signedIn = true;
  const scope = new RequestScope(),
    first = scope.begin(),
    second = scope.begin();
  assert.equal(first.current(), false);
  assert.equal(first.signal.aborted, true);
  assert.equal(second.current(), true);
  Session.epoch++;
  assert.equal(second.current(), false);
  scope.dispose();
  assert.equal(second.signal.aborted, true);
});

test("preferences handle old strings, corruption, unavailable storage and failed writes", () => {
  const values = new Map([
    ["density", "compact"],
    ["bad", "{not json"],
    ["flag", "false"],
  ]);
  globalThis.localStorage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: () => {
      throw Error("storage denied");
    },
    removeItem: () => {},
  };
  assert.equal(Preferences.read("density", "normal"), "compact");
  assert.deepEqual(Preferences.read("bad", {}), {});
  assert.equal(Preferences.read("flag", true), false);
  Preferences.write("density", "normal");
  assert.equal(Preferences.read("density", "normal"), "normal");
  globalThis.localStorage.getItem = () => {
    throw Error("storage denied");
  };
  assert.equal(Preferences.read("unknown", false), false);
  Preferences.setLanguage("en");
  assert.equal(Preferences.language(), "en");
  delete globalThis.localStorage;
});

test("HTTP error normalization preserves status and never retries mutations", async () => {
  const original = globalThis.fetch;
  let calls = 0;
  try {
    for (const status of [401, 403, 409, 500, 502]) {
      globalThis.fetch = async () => {
        calls++;
        return new Response("<html>Proxy failure</html>", { status });
      };
      const before = calls;
      await assert.rejects(
        request("test", { action: "buy" }),
        (error) => error.status === status && error.kind === "http",
      );
      assert.equal(calls - before, 1);
    }
    globalThis.fetch = async () => new Response("not JSON", { status: 200 });
    await assert.rejects(
      request("test"),
      (error) => error.kind === "protocol" && error.status === 200,
    );
    globalThis.fetch = async () => {
      throw new DOMException("aborted", "AbortError");
    };
    await assert.rejects(
      request("test"),
      (error) => error.name === "AbortError",
    );
  } finally {
    globalThis.fetch = original;
  }
});

test("public asset cache deduplicates a load and retries failed assets", async () => {
  const cache = new AssetCache(6);
  let count = 0;
  const first = cache.load("art", async () => {
    count++;
    return 1;
  });
  assert.equal(
    first,
    cache.load("art", () => 2),
  );
  assert.equal(await first, 1);
  assert.equal(count, 1);
  await assert.rejects(
    cache.load("failure", () => {
      throw Error("missing");
    }),
  );
  assert.equal(await cache.load("failure", () => 3), 3);
});

test("formatting is localized without reducing scientific values or changing DTOs", () => {
  I18n.language = "ru";
  const dto = { value: 0.0078 };
  assert.equal(Format.science(dto.value), "0,0078");
  assert.equal(Format.percent(50.2), "50,2%");
  assert.equal(Format.number(Infinity), "—");
  I18n.catalogs.ru = { "Привет, {name}": "Привет, {name}" };
  assert.equal(I18n.t("Привет, {name}", { name: "Пилот" }), "Привет, Пилот");
  assert.equal(dto.value, 0.0078);
});
