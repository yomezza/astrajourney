import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";

// One safe AST renderer for reading and administrator previews; no raw HTML.
export function renderBlocks(host, blocks, { open, assets = {} } = {}) {
  const n = UI.node,
    tr = (s) => I18n.t(s);
  const inline = (parent, parts) => {
    for (const v of parts || []) {
      if (v.type === "text")
        parent.append(document.createTextNode(v.text || ""));
      else if (["emphasis", "strong", "code"].includes(v.type)) {
        const node = n(v.type === "emphasis" ? "em" : v.type);
        inline(node, v.children);
        parent.append(node);
      } else if (v.type === "link") {
        if (
          !/^(article:[0-9a-f-]{36}|game:(resource|ship|object|technical):[a-z0-9-]+)$/.test(
            v.target || "",
          )
        )
          continue;
        const button = UI.button(v.text, () =>
          open?.(
            v.target.startsWith("article:") ? v.target.slice(8) : v.target,
          ),
        );
        button.classList.add("guide-inline-link");
        parent.append(button);
      } else if (v.type === "image") {
        let src;
        if (/^media:[0-9a-f-]{36}$/.test(v.target || ""))
          src = "/api/guide/media/" + v.target.slice(6);
        else if (v.target?.startsWith("builtin:"))
          src = assets[v.target.slice(8)]?.path;
        if (!src) {
          parent.append(n("span", tr("Ресурс недоступен"), "muted"));
          continue;
        }
        const img = n("img");
        img.src = src;
        img.alt = v.text || "";
        img.loading = "lazy";
        img.className = "guide-illustration";
        img.onerror = () =>
          img.replaceWith(n("span", tr("Ресурс недоступен"), "muted"));
        parent.append(img);
      }
    }
  };
  for (const block of blocks || []) {
    const tag =
      block.type === "heading"
        ? "h" + Math.max(2, Math.min(4, block.level || 2))
        : block.type === "code"
          ? "pre"
          : block.type === "list_item"
            ? "li"
            : "p";
    const node = n(tag);
    inline(node, block.content);
    host.append(node);
  }
}
