"""Shared colony catalogue source and mock-world consistency checks.

Run: python -m unittest tests.test_colony_catalog -v
No Go services, PostgreSQL, or browser required.
"""
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
sys.path.insert(0, str(ROOT))
from mock import data, registry


class ColonyCatalogueTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.demo_js = (ROOT / "mock/colony-catalog.js").read_text(encoding="utf-8")
        cls.game_js = (REPO / "internal/server/web/features/planet/colony-catalog.js").read_text(encoding="utf-8") if (REPO / "internal/server/web/features/planet/colony-catalog.js").exists() else None
        cls.demo_css = (ROOT / "mock/colony-catalog.css").read_text(encoding="utf-8")
        cls.game_css = (REPO / "internal/server/web/styles/colony-catalog.css").read_text(encoding="utf-8") if (REPO / "internal/server/web/styles/colony-catalog.css").exists() else None
        cls.overlay = (ROOT / "mock/ui-overlay.js").read_text(encoding="utf-8")

    def test_shared_sources_match_when_game_checkout_is_present(self):
        if self.game_js is not None:
            self.assertEqual(self.demo_js, self.game_js)
        if self.game_css is not None:
            self.assertEqual(self.demo_css, self.game_css)

    def test_existing_planet_types_are_mapped(self):
        for kind, filename in {
            "Каменистая": "object-rocky.svg",
            "Садовый мир": "object-garden.svg",
            "Пустынная": "object-desert.svg",
            "Ледяная": "object-icy.svg",
            "Лавовая": "object-lava.svg",
            "Вулканическая": "object-lava.svg",
        }.items():
            self.assertIn(f'"{kind}":"{filename}"', self.demo_js)
        self.assertIn("object-unknown.svg", self.demo_js)
        self.assertIn("system?.sites?.find", self.demo_js)
        self.assertNotIn("Math.random()", self.demo_js)

    def test_mock_projects_resolve_to_real_registry_planets(self):
        for project in data.COLONY_PROJECTS:
            with self.subTest(project=project["id"]):
                self.assertIn(project["star"], registry.STAR_BY_ID)
                matches = [site for site in registry.SYSTEM_SITES[project["star"]]
                           if site["planet"] == project["planet"]
                           and site["kind"] in {
                               "Каменистая", "Садовый мир", "Пустынная",
                               "Ледяная", "Лавовая", "Вулканическая", "Газовый гигант"
                           }]
                self.assertEqual(len(matches), 1)

    def test_selection_and_actions_remain_bound_to_existing_ids(self):
        for expression in (
            "state.selected=p.id", "options.onProject(p.id)",
            "options.onSurface(p.star,p.planet)", "options.onVisit",
            "options.onRefund(refund)", 'aria-pressed',
            'options.loadSystem(p.star)', "state.cache",
        ):
            self.assertIn(expression, self.demo_js)
        self.assertIn("openColonyCatalogue", self.overlay)
        self.assertIn("api('planet/projects')", self.overlay)
        self.assertIn("api('system?id='", self.overlay)

    def test_responsive_container_and_empty_state(self):
        self.assertIn("container-type:inline-size", self.demo_css)
        self.assertIn("@container(max-width:670px)", self.demo_css)
        self.assertIn("Нет доступных колоний или проектов колонизации", self.demo_js)


if __name__ == "__main__":
    unittest.main()
