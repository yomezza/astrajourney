"""Research system lineup: source-registry contract and demo integration.

Run: python -m unittest tests.test_studies_system_view -v
Does not need Go, PostgreSQL or a real game account.
"""
import json
import shutil
import subprocess
import sys
import threading
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
sys.path.insert(0, str(ROOT))
from server import DemoHandler
from mock import registry


class Quiet(DemoHandler):
    def log_message(self, *_):
        pass


class SystemViewTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.prototype = ROOT / "prototype" / "studies"
        cls.view = (cls.prototype / "system-view.js").read_text(encoding="utf-8")
        cls.app = (cls.prototype / "app.js").read_text(encoding="utf-8")
        cls.style = (cls.prototype / "system-view.css").read_text(encoding="utf-8")

    def test_all_real_svg_models_have_an_explicit_mapping(self):
        for kind in (
            "Ледяная", "Каменистая", "Садовый мир", "Газовый гигант",
            "Пустынная", "Лавовая", "Вулканическая",
            "Орбитальная станция", "Пояс астероидов", "Ледяное поле",
            "Чёрная дыра",
        ):
            with self.subTest(kind=kind):
                self.assertIn(f'"{kind}"', self.view)
        for star_class in "OBAFGKM":
            self.assertIn(f'"{star_class}"', self.view)
        self.assertIn("object-unknown.svg", self.view)
        self.assertIn("assetFor(", self.view)
        self.assertIn('art.loading=i<4?"eager":"lazy"', self.view)
        self.assertIn('art.decoding="async"', self.view)
        self.assertNotIn("canvas", self.view.lower())
        self.assertNotIn("three.js", self.view.lower())

    def test_details_use_existing_system_endpoint(self):
        self.assertIn("fetch('/api/system?id='", self.app)
        self.assertIn("data.star.id!==id", self.app)
        self.assertIn("Array.isArray(data?.sites)", self.view)
        self.assertIn("orderSites(sites)", self.view)
        self.assertIn("data.surveyed", self.view)
        self.assertIn("data.sites.filter(valid)", self.view)
        self.assertIn("resolveResource(", self.view)
        self.assertNotIn("random()", self.view)
        self.assertNotIn("Math.random()", self.view)

    def test_inwindow_navigation_and_preservation(self):
        self.assertIn('class="study-entry-actions"', self.app)
        self.assertIn('id="detail-more"', self.app)
        self.assertIn('class="detail-more-btn terminal-action"', self.app)
        self.assertIn("onBack:()=>{systemView=null;markSelection();}", self.app)
        self.assertIn("previous.forEach(node=>node.hidden=true)", self.view)
        self.assertIn("node.hidden=hiddenValues[i]", self.view)
        self.assertIn("host.append(view)", self.view)
        self.assertIn("controller.abort()", self.view)
        self.assertIn("tooltip.remove()", self.view)
        self.assertIn("openOnMap(system)", self.app)
        self.assertIn("new URLSearchParams(location.search).has('embed')", self.app)
        self.assertNotIn("Modules.create(", self.view)
        self.assertNotIn("<iframe", self.view)

    def test_tooltip_keyboard_touch_and_viewport_clamp(self):
        for term in (
            "getBoundingClientRect()", "document.body.append(tip)",
            'button.addEventListener("pointerenter"',
            'button.addEventListener("focus"',
            'button.addEventListener("click"',
            'if(e.key==="Escape"', "signal",
            "Number.isFinite", 'tip.setAttribute("role","tooltip")',
        ):
            self.assertIn(term, self.view)
        self.assertIn("Math.max(margin,Math.min(", self.view)
        self.assertIn(".research-system-tooltip[hidden]", self.style)
        self.assertIn("overflow-x:auto", self.style)
        self.assertIn("@media(max-width:420px)", self.style)

    def test_registry_objects_are_not_fabricated(self):
        for sid in (1, 2, 3, 19, 70):
            with self.subTest(star=sid):
                star = registry.STAR_BY_ID[sid]
                data = registry.system(sid)
                self.assertEqual(data["star"]["id"], star["id"])
                real = registry.SYSTEM_SITES[sid]
                if sid in registry.KNOWN_IDS:
                    self.assertEqual([s["id"] for s in data["sites"]],
                                     [s["id"] for s in real])
                else:
                    self.assertFalse(data["sites"])

    def test_live_http_preserves_known_and_unknown_sites(self):
        http = ThreadingHTTPServer(("127.0.0.1", 0), Quiet)
        thread = threading.Thread(target=http.serve_forever, daemon=True)
        thread.start()
        try:
            def read(id):
                with urlopen(f"http://127.0.0.1:{http.server_port}/api/system?id={id}",
                             timeout=5) as response:
                    return json.load(response)
            known = read(3)
            self.assertEqual(known["star"]["id"], 3)
            self.assertTrue(known["sites"])
            self.assertEqual([s["id"] for s in known["sites"]],
                             [s["id"] for s in registry.SYSTEM_SITES[3]])
            self.assertEqual(read(70)["sites"], [])
        finally:
            http.shutdown()
            http.server_close()
            thread.join(timeout=2)

    def test_css_and_js_are_served_through_demo(self):
        http = ThreadingHTTPServer(("127.0.0.1", 0), Quiet)
        thread = threading.Thread(target=http.serve_forever, daemon=True)
        thread.start()
        try:
            base=f"http://127.0.0.1:{http.server_port}"
            for path in ("/prototype/studies/system-view.js",
                         "/prototype/studies/system-view.css",
                         "/prototype/studies/object-unknown.svg"):
                with self.subTest(path=path), urlopen(base+path, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    self.assertGreater(len(response.read()), 100)
        finally:
            http.shutdown()
            http.server_close()
            thread.join(timeout=2)

    def test_original_and_demo_share_same_renderer(self):
        actual = REPO / "internal" / "server" / "web" / "features" / "research" / "system-view.js"
        if not actual.is_file():
            self.skipTest("Only standalone frontend-generation installed")
        self.assertEqual(actual.read_bytes(),
                         (self.prototype / "system-view.js").read_bytes())
        production = (REPO / "internal" / "server" / "web" /
                      "features" / "research" / "index.js").read_text("utf-8")
        self.assertIn('mountSystemLineup } from "./system-view.js"', production)
        self.assertIn('b("Подробнее", () => showSystem(item.star))', production)

    @unittest.skipUnless(shutil.which("node"), "Node.js not installed")
    def test_js_syntax(self):
        scripts = [self.prototype / "system-view.js", self.prototype / "app.js"]
        if (REPO / "internal" / "server" / "web" / "features" / "research" / "index.js").is_file():
            scripts += [REPO / "internal" / "server" / "web" /
                        "features" / "research" / "index.js"]
        for script in scripts:
            with self.subTest(script=script):
                result = subprocess.run(
                    ["node", "--check", str(script)], capture_output=True,
                    text=True, timeout=10
                )
                self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
