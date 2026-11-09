"""News hologram regression tests: exact artist assets, one widget, shared fixture."""
from datetime import datetime
from hashlib import sha1
from pathlib import Path
import json
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
NEWS = ROOT / "mock" / "news"

# Original Git blob identities from docs/ideas/news, not hashes of a rerender.
ART = {
    "news-projector-base.svg": "661066409d2833dbec520fe3f24d0cb493fdff16",
    "news-projection-beams.svg": "4761fbc2675c3bc247c9b0147c4cdeea38bdb926",
    "news-panel-left.svg": "70292966e0ce2f6a55be5080232f4bee6f7f574f",
    "news-panel-right.svg": "fc4d4b63beff808bd40ab0b5dbcffada86c69a10",
    "news-all-button.svg": "1e7977c5c5dbf4d1244f5c48ef5120237117fba6",
}


class NewsIntegration(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads((NEWS / "news.json").read_text(encoding="utf-8"))
        cls.js = (NEWS / "news.js").read_text(encoding="utf-8")
        cls.css = (NEWS / "news.css").read_text(encoding="utf-8")
        cls.overlay = (ROOT / "mock" / "ui-overlay.js").read_text(encoding="utf-8")
        cls.chat = (ROOT / "mock" / "chat-native.js").read_text(encoding="utf-8")
        cls.server = (ROOT / "server.py").read_text(encoding="utf-8")

    def test_exact_five_unchanged_art_sources(self):
        for name, expected_sha in ART.items():
            with self.subTest(svg=name):
                payload = (NEWS / "assets" / name).read_bytes()
                self.assertIn(b"<svg", payload)
                actual_sha = sha1(b"blob " + str(len(payload)).encode() + b"\0" + payload).hexdigest()
                self.assertEqual(actual_sha, expected_sha)

    def test_fixture_is_fictional_unique_bilingual_and_time_stamped(self):
        from mock import registry
        self.assertTrue(self.data["fictional"])
        self.assertGreaterEqual(len(self.data["items"]), 8)
        self.assertLessEqual(len(self.data["items"]), 12)
        ids = set()
        for item in self.data["items"]:
            with self.subTest(item=item["id"]):
                self.assertNotIn(item["id"], ids)
                ids.add(item["id"])
                self.assertRegex(item["id"], r"^mock-news-\d{3}$")
                self.assertIn(item["star_id"], registry.STAR_BY_ID)
                if "object_id" in item:
                    self.assertIn(item["object_id"],
                        {s["id"] for s in registry.SYSTEM_SITES[item["star_id"]]})
                self.assertIn(item["category"], (
                    "research", "trade", "galaxy", "colonies", "economy",
                    "corporations", "events"))
                self.assertIn(item["icon"], ("star", "market", "planet"))
                moment = datetime.fromisoformat(item["published_at"].replace("Z", "+00:00"))
                self.assertIsNotNone(moment.tzinfo)
                for language in ("ru", "en"):
                    self.assertTrue(item[language]["title"])
                    self.assertTrue(item[language]["category"])
                    self.assertIsInstance(item[language]["summary"], str)

    def test_scenario_coverage(self):
        self.assertTrue(any(len(item["ru"]["title"]) > 85 for item in self.data["items"]))
        self.assertTrue(any(not item["ru"]["summary"] for item in self.data["items"]))
        for mode in ("normal", "long", "nodesc", "one", "empty", "error"):
            self.assertIn("'" + mode + "'", self.js)
        self.assertIn("scenarioItems(allNews,scenario)", self.js)

    def test_single_composition_and_original_layers(self):
        self.assertIn("const stage=el('div','news-stage')", self.js)
        for filename in ART:
            self.assertIn(filename, self.js)
        self.assertIn("const panels=['left','right']", self.js)
        self.assertIn("const root=el('section','news-hologram-widget variant-'+variant)", self.js)
        self.assertIn("slot.replaceChildren(root)", self.js)
        self.assertIn("const overlays = new WeakMap()", self.js)
        self.assertNotIn("<iframe", self.js)
        self.assertNotIn("innerHTML=html", self.js)
        self.assertNotIn("fetch('https://", self.js)

    def test_switch_only_changes_variant_not_stories(self):
        self.assertIn("Preferences.read(KEY,'b'", self.js)
        self.assertIn("Preferences.write(KEY,next)", self.js)
        self.assertIn("root.classList.replace('variant-'+variant,'variant-'+next)", self.js)
        self.assertIn("select.addEventListener('change'", self.js)
        self.assertEqual(self.js.count("slot.replaceChildren(root)"), 1)
        self.assertIn("window.addEventListener('languagechange',render", self.js)
        self.assertIn("state==='error'", self.js)

    def test_real_modules_window_and_locale(self):
        self.assertIn("Modules.create('news-all-demo'", self.js)
        self.assertIn("Modules.open('news-all-demo')", self.js)
        self.assertIn("Modules.title('news-all-demo'", self.js)
        self.assertIn("document.createElement(tag)", self.js)
        self.assertIn("time.dateTime=item.published_at", self.js)
        self.assertIn("lang==='ru'?'Все\\nновости':'All\\nnews'", self.js)

    def test_chat_docking_independent_of_news(self):
        self.assertIn("grid.id='home-lower-grid';grid.hidden=false", self.overlay)
        self.assertIn("slot.id='chat-home-slot';slot.hidden=true", self.overlay)
        self.assertIn("import('/mock/news/news.js')", self.overlay)
        self.assertIn(" $('chat-home-slot').hidden=!dock;", self.chat)
        self.assertIn(" $('home-lower-grid').hidden=false;", self.chat)
        self.assertIn("$('chat-home-slot').hidden=true;$('home-lower-grid').hidden=false", self.chat)
        self.assertIn("#news-home-slot{display:block!important", self.css)
        self.assertIn("#chat-home-slot[hidden]", self.css)
        self.assertIn("body:not(.chat-docked)", self.css)
        self.assertNotIn("Modules.create('chat-v13-mock'", self.overlay)
        self.assertIn("studies-v13-mock", self.overlay)

    def test_css_is_scoped_and_injected_after_chat(self):
        self.assertIn(".news-hologram-widget.variant-a", self.css)
        self.assertIn(".news-hologram-widget.variant-b", self.css)
        self.assertIn(".news-hologram-widget.variant-c", self.css)
        self.assertIn("@container news-stage (max-width:440px)", self.css)
        self.assertIn("prefers-reduced-motion:reduce", self.css)
        self.assertIn(".module-content.news-demo-window", self.css)
        self.assertNotRegex(self.css, r"(?m)^\s*(?:body|main|header|button|select)\s*\{")
        self.assertIn('<link rel="stylesheet" href="/mock/news/news.css">', self.server)

    def test_loads_only_local_fixture_without_background_polling(self):
        self.assertIn("fetch(ROOT+'news.json'", self.js)
        self.assertNotIn("setInterval(", self.js)
        self.assertNotIn("postMessage(", self.js)
        self.assertIn("AbortController()", self.js)
        self.assertIn("window.addEventListener('pagehide'", self.js)
        self.assertNotIn("/api/", self.js)

    def test_server_serves_news_from_mock_mount(self):
        from http.server import ThreadingHTTPServer
        from urllib.request import urlopen
        from server import DemoHandler
        import threading

        class QuietHandler(DemoHandler):
            def log_message(self, *args):
                pass

        http = ThreadingHTTPServer(("127.0.0.1", 0), QuietHandler)
        thread = threading.Thread(target=http.serve_forever, daemon=True)
        thread.start()
        try:
            url = "http://127.0.0.1:" + str(http.server_port)
            with urlopen(url + "/mock/news/news.json", timeout=3) as response:
                self.assertIn("no-store", response.headers["Cache-Control"])
                result = json.load(response)
                self.assertEqual(len(result["items"]), len(self.data["items"]))
            with urlopen(url + "/mock/news/assets/news-panel-left.svg", timeout=3) as response:
                self.assertIn("image/svg+xml", response.headers["Content-Type"])
                self.assertEqual(
                    sha1(b"blob " + str(len(payload := response.read())).encode() +
                         b"\0" + payload).hexdigest(),
                    ART["news-panel-left.svg"])
        finally:
            http.shutdown()
            http.server_close()
            thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
