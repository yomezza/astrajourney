"""UI/UX laboratory: registry integrity and non-invasive integration."""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LAB = ROOT / "mock" / "ui-lab"
REGISTRY = LAB / "catalog.json"
ID = re.compile(r"^ui\.[a-z0-9-]+(?:\.[a-z0-9-]+)+$")
VALID = {"original", "approved", "adapter", "pending"}


class UiLabTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads(REGISTRY.read_text(encoding="utf-8"))

    def test_canonical_ids_are_unique_and_have_translations(self):
        components = self.data["components"]
        self.assertGreaterEqual(len(components), 140)
        ids = [item["id"] for item in components]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertTrue(all(ID.fullmatch(name) for name in ids))
        for item in components:
            with self.subTest(item=item["id"]):
                for field in ("name_ru", "name_en", "short_ru", "short_en",
                              "renderer", "variant", "category", "subcategory",
                              "implementation", "description_ru", "description_en"):
                    self.assertTrue(item[field])
                self.assertIn(item["status"], VALID)
                self.assertTrue(item["source"])
                self.assertIsInstance(item["states"], list)
                self.assertIsInstance(item["uses"], list)
                self.assertIsInstance(item["css"], list)

    def test_categories_and_counts_are_derived_from_components(self):
        categories = self.data["categories"]
        self.assertGreaterEqual(len(categories), 14)
        ids = {item["id"] for item in categories}
        for category in categories:
            count = sum(item["category"] == category["id"] for item in self.data["components"])
            self.assertEqual(category["count"], count, category["id"])
            self.assertGreater(count, 0)
        self.assertEqual(ids, {item["category"] for item in self.data["components"]})

    def test_references_do_not_escape_source_roots(self):
        roots = ("internal/server/web/", "scripts/frontend-generation/")
        for item in self.data["components"]:
            for source in item["source"] + item["sourceJs"] + item["sourceCss"]:
                with self.subTest(item=item["id"], source=source):
                    self.assertTrue(source.startswith(roots))
                    self.assertNotIn("..", source)
            self.assertEqual(item["sourceJs"], [s for s in item["source"] if s.endswith(".js")])
        self.assertIn("ui.context-menu.compact", {i["id"] for i in self.data["components"]})
        self.assertIn("ui.context-menu.studies-source", {i["id"] for i in self.data["components"]})

    def test_live_renderer_and_source_anchors(self):
        code = (LAB / "lab.js").read_text(encoding="utf-8")
        css = (LAB / "lab.css").read_text(encoding="utf-8")
        for marker in ("UI.menu(", "UI.picker(", "UI.choice(", "UI.table(",
                       "UI.tabs(", "UI.bar(", "UI.hologram(", "Modules.create(",
                       "actionAmount(", "paintFlag(", "showCompare(", "copy(",
                       "requestAnimationFrame(", "menuCleanup("):
            self.assertIn(marker, code, marker)
        self.assertIn(".uiux-root", css)
        self.assertIn(".uiux-sample-popup", css)
        self.assertIn(".studies-source-kkm", css)
        self.assertIn("chat-context-menu", code)
        self.assertIn("studies-source-kkm", code)
        self.assertNotIn("fetch('/api/workbench'", code)

    def test_lab_scroll_is_isolated_from_page_layout(self):
        code = (LAB / "lab.js").read_text(encoding="utf-8")
        css = (LAB / "lab.css").read_text(encoding="utf-8")
        # The game's global header/main selectors must not affect lab panels.
        self.assertIn("const main=$('div',null,'uiux-main')", code)
        self.assertIn("const toolbar=$('div',null,'uiux-toolbar')", code)
        self.assertNotIn("const main=$('main',null,'uiux-main')", code)
        self.assertNotIn("const toolbar=$('header',null,'uiux-toolbar')", code)
        self.assertIn(".uiux-content{flex:1;overflow-y:auto;overflow-x:hidden", css)
        self.assertIn(".uiux-category-scroll{flex:1;overflow-y:auto", css)
        self.assertIn("overscroll-behavior:contain", css)
        self.assertIn("overscroll-behavior-y:contain", css)
        self.assertIn(".uiux-root{", css)
        self.assertIn("height:100%;width:100%;min-width:0;min-height:0;overflow:hidden", css)

    def test_integration_does_not_wrap_chat(self):
        integration = (ROOT / "mock" / "ui-overlay.js").read_text("utf-8")
        self.assertIn("import('/mock/ui-lab/lab.js')", integration)
        self.assertIn("bootUiLab", integration)
        self.assertIn("await import('/mock/chat-native.js')", integration)
        self.assertNotIn("Modules.create('chat-v13-mock'", integration)
        self.assertIn("studies-v13-mock", integration)
        self.assertNotIn("frame.src='/prototype/chat/index.html'", integration)

    def test_approved_snapshots_stay_available(self):
        self.assertTrue((ROOT / "prototype" / "approved-chat-v13.html").is_file())
        self.assertTrue((ROOT / "prototype" / "approved-studies-v13-offline.html").is_file())
        self.assertEqual(self.data["source_root"], "internal/server/web/")


if __name__ == "__main__":
    unittest.main()
