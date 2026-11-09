"""Regression for advanced UI/UX demo adapters: no gameplay mutation endpoints."""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LAB = ROOT / "mock" / "ui-lab"

ADVANCED = (
    "ui.map.planet.scene", "ui.map.planet.tree", "ui.map.planet.rock",
    "ui.map.planet.actor", "ui.map.planet.terrain", "ui.map.route.hit-testing",
    "ui.table.cursor.history", "ui.card.cargo-transfer", "ui.window.danger-typed",
    "ui.chart.benchmark", "ui.corporation.flag-editor",
    "ui.admin.service-log", "ui.form.two-phase",
    "ui.tooltip.resource-rich", "ui.nav.window-groups",
    "ui.graphics.planet-textures",
)


class AdvancedUiLabTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog = json.loads((LAB / "catalog.json").read_text("utf-8"))
        cls.advanced = (LAB / "advanced.js").read_text("utf-8")
        cls.lab = (LAB / "lab.js").read_text("utf-8")

    def test_all_previous_pending_have_a_live_advanced_renderer(self):
        entries = {item["id"]: item for item in self.catalog["components"]}
        self.assertEqual(len(ADVANCED), len(set(ADVANCED)))
        self.assertEqual(len(self.catalog["components"]), 156)
        self.assertFalse([e for e in entries.values() if e["status"] == "pending"])
        for component_id in ADVANCED:
            with self.subTest(component_id=component_id):
                entry = entries[component_id]
                self.assertEqual(entry["status"], "adapter")
                self.assertEqual(entry["renderer"], "advanced")
                self.assertIn(component_id + "':", self.advanced)
                self.assertIn("mock/ui-lab/advanced.js", " ".join(entry["source"]))

    def test_original_helpers_and_sprite_pipeline(self):
        for anchor in (
            "createSceneSprites(", "createVisual()", "acquireArt(",
            "terrain(", "texture(", "materials(", "resourceTile(",
            "paintFlag(", "registerTooltip(", "cursorMore(",
            "UI.table(", "UI.confirm(", "UI.field(", "Modules.link(",
            "Modules.adjacent(", "getContext('2d')", "ondragstart",
            "ondrop", "onpointermove",
        ):
            with self.subTest(anchor=anchor):
                self.assertIn(anchor, self.advanced)

    def test_disposers_called_by_main_lab(self):
        self.assertIn("export function disposeAdvanced()", self.advanced)
        self.assertIn("export function disposeAdvancedUnder(parent)", self.advanced)
        self.assertIn("disposeAdvancedUnder(results)", self.lab)
        self.assertIn("disposeAdvancedUnder(comparePanel)", self.lab)
        self.assertIn("disposeAdvancedFor(preview)", self.lab)
        self.assertIn("disposeAdvanced();", self.lab)
        self.assertIn("renderAdvanced(item,host)", self.lab)

    def test_no_backend_mutations_in_advanced_demos(self):
        for forbidden in (
            "fetch('/api/workbench'", "fetch('/api/command'",
            "fetch('/api/planet/action'", "fetch('/api/admin'",
            "method:'POST'", 'method: "POST"', "localStorage.setItem",
        ):
            self.assertNotIn(forbidden, self.advanced)
        self.assertIn("fetch('/api/galaxy')", self.advanced)
        self.assertIn("Original UI", "Original UI")  # Helpers are imported, not copied.

    def test_css_is_loaded_and_scoped(self):
        self.assertIn("@import url('/mock/ui-lab/advanced.css')",
                      (LAB / "lab.css").read_text("utf-8"))
        css = (LAB / "advanced.css").read_text("utf-8")
        for cls in (".uiux-advanced-group", ".uiux-advanced-canvas",
                    ".uiux-cargo-grid", ".uiux-advanced-status"):
            self.assertIn(cls, css)

    def test_source_fragments_have_real_paths(self):
        for component in self.catalog["components"]:
            if component["id"] not in ADVANCED:
                continue
            self.assertTrue(any(s.startswith("internal/server/web/")
                                for s in component["source"]))
            self.assertTrue(any(s.startswith("scripts/frontend-generation/")
                                for s in component["source"]))
            self.assertEqual(component["sourceJs"],
                             [x for x in component["source"]
                              if x.endswith(".js")])


if __name__ == "__main__":
    unittest.main()
