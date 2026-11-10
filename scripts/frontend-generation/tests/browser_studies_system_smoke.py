#!/usr/bin/env python3
"""Optional real Chromium smoke for Studies v13 system lineup.

Prerequisites: start frontend-generation on localhost:8765 and install Playwright.
Run: python tests/browser_studies_system_smoke.py
No real game backend is contacted.
"""
import json
import os
from pathlib import Path
from urllib.request import urlopen

from playwright.sync_api import sync_playwright

URL = os.environ.get("FRONTEND_DEMO_URL", "http://127.0.0.1:8765").rstrip("/")
OUT = Path(os.environ.get("STUDIES_SCREENSHOT_DIR", "/tmp/studies-system"))
OUT.mkdir(parents=True, exist_ok=True)


def fetch(id):
    with urlopen(f"{URL}/api/system?id={id}", timeout=8) as response:
        return json.load(response)


def run():
    expected = fetch(3)
    assert expected["sites"], "System #3 should have revealed sites in fixture"
    broken = []
    errors = []
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        for width, height in ((1440, 900), (880, 800), (390, 844)):
            page = browser.new_page(viewport={"width": width, "height": height})
            page.on("pageerror", lambda e: errors.append(str(e)))
            page.on("response", lambda r: broken.append((r.status, r.url))
                    if r.status >= 400 and ("/object-" in r.url or "/prototype/studies" in r.url)
                    else None)
            page.goto(URL+"/prototype/studies/index.html?embed=1",
                      wait_until="domcontentloaded")
            page.locator('.study-entry[data-star="3"] .detail-more-btn').wait_for(timeout=15000)
            page.locator('.study-entry[data-star="3"] .detail-more-btn').click()
            cards = page.locator(".research-system-object")
            assert cards.count() == len(expected["sites"])+1, (
                width, cards.count(), len(expected["sites"])
            )
            assert cards.first.get_attribute("data-kind") == "Звезда"
            assert cards.first.locator("img").get_attribute("src").endswith(
                "object-star-"+expected["star"]["class"].lower()+".svg"
            )
            actual_ids=[int(card.get_attribute("data-object-id")) for card in cards.all()]
            assert actual_ids[1:] == [site["id"] for site in sorted(
                expected["sites"], key=lambda site: site["orbit"]
            )]
            # First image must load; the original SVG is essential.
            cards.first.locator("img").evaluate(
                "(img) => new Promise((resolve,reject)=>{" +
                "if(img.complete)return img.naturalWidth?resolve():reject(new Error(img.src));" +
                "img.onload=()=>resolve();img.onerror=()=>reject(new Error(img.src));})"
            )
            cards.first.locator("button").hover()
            assert page.locator(".research-system-tooltip:not([hidden])").count() == 1
            assert "Альтаир" in page.locator(".research-system-tooltip").inner_text()
            tooltip = page.locator(".research-system-tooltip").bounding_box()
            assert tooltip["x"] >= 0 and tooltip["x"] + tooltip["width"] <= width+1
            page.screenshot(path=str(OUT/f"system-{width}.png"),full_page=False)
            page.locator(".research-system-back").click()
            assert page.locator(".research-system-view").count()==0
            assert page.locator('#studies-search').is_visible()
            assert page.locator('.study-entry[data-star="3"] .detail-more-btn').is_visible()
            page.close()
        browser.close()
    assert not broken, "Missing original SVG/prototype resource: "+str(broken)
    assert not errors, "JavaScript exceptions: "+str(errors)
    print("OK: 1440, 880, 390; API site IDs/order, original SVG, tooltip bounds, back")
    print("Screenshots:", OUT)


if __name__ == "__main__":
    run()
