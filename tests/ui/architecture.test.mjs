import assert from "node:assert/strict";
import { test } from "node:test";
import { readFile, readdir, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parse } from "acorn";
import { analyze } from "eslint-scope";
import globals from "globals";

const root = new URL("../../web/", import.meta.url);
const allowed = new Set([
  ...Object.keys(globals.browser),
  ...Object.keys(globals.es2026),
]);

test("ES module graph has no missing dependencies, implicit globals or reversed UI dependencies", async () => {
  const failures = [];
  for (const name of (await readdir(root, { recursive: true })).filter((f) =>
    f.endsWith(".js"),
  )) {
    const url = new URL(name, root),
      source = await readFile(url, "utf8");
    const tree = parse(source, {
      ecmaVersion: "latest",
      sourceType: "module",
      ranges: true,
      locations: true,
    });
    const visit = (node) => {
      if (!node || typeof node !== "object") return;
      if (
        node.type === "MemberExpression" &&
        ["window", "globalThis", "self"].includes(node.object?.name)
      ) {
        const key = node.computed ? node.property.value : node.property.name;
        if (typeof key === "string" && !allowed.has(key))
          failures.push(`${name}: undocumented global ${key}`);
      }
      if (
        node.type === "CallExpression" &&
        node.callee.object?.name === "Object" &&
        node.callee.property?.name === "assign" &&
        node.arguments[1]?.properties?.some((p) => p.kind === "get")
      ) {
        failures.push(`${name}: Object.assign flattens live getters`);
      }
      for (const value of Object.values(node)) {
        if (Array.isArray(value)) value.forEach(visit);
        else if (value && typeof value === "object") visit(value);
      }
    };
    visit(tree);
    const scope = analyze(tree, {
      ecmaVersion: 2024,
      sourceType: "module",
      optimistic: true,
    });
    for (const ref of scope.globalScope.through)
      if (!allowed.has(ref.identifier.name))
        failures.push(
          `${name}:${ref.identifier.loc.start.line}: implicit ${ref.identifier.name}`,
        );
    for (const entry of tree.body.filter(
      (n) => n.type === "ImportDeclaration",
    )) {
      const specifier = entry.source.value;
      assert.ok(
        specifier.startsWith("."),
        `${name}: external runtime dependency ${specifier}`,
      );
      const target = new URL(specifier, url);
      assert.ok(
        target.href.startsWith(root.href),
        `${name}: import outside web`,
      );
      assert.ok((await stat(target)).isFile(), `${name}: missing ${specifier}`);
      const path = fileURLToPath(target).slice(fileURLToPath(root).length);
      if (name.startsWith("ui/") && path.startsWith("features/"))
        failures.push(`${name}: generic UI imports ${path}`);
      if (name.startsWith("core/") && /^(ui|features)\//.test(path))
        failures.push(`${name}: core imports ${path}`);
    }
    assert.equal(
      /window\.(?:UI|Modules|Game|gameData|MapData)\s*=/.test(source),
      false,
      `${name}: legacy mutable window export`,
    );
  }
  assert.deepEqual(failures, []);
  const html = await readFile(new URL("index.html", root), "utf8");
  assert.match(html, /<script type="module" src="bootstrap\.js"><\/script>/);
  assert.equal(/<script[^>]*\bdefer\b/.test(html), false);
});
