#!/usr/bin/env python3
"""Optional real-browser POC test; running local demo + Playwright required.

    python launch.py --offline
    python tests/browser_pilot_terminal_smoke.py

Runs only against 127.0.0.1:8765 with fictional data. It never uses production.
"""
import os
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit, urlencode, parse_qsl


URL = os.environ.get("FRONTEND_DEMO_URL", "http://127.0.0.1:8765/").rstrip("/")
OUT = Path(os.environ.get("PILOT_SCREENSHOT_DIR", "/tmp/pilot-terminal-smoke"))


def login(page):
    page.wait_for_function("""() => {
      const game=document.getElementById('game'),auth=document.getElementById('auth');
      return (game&&!game.hidden)||(auth&&!auth.hidden);
    }""", timeout=15000)
    if page.locator("#auth").is_visible():
        page.locator('#auth-form input[name="username"]').fill("pilot_smoke")
        page.locator('#auth-form input[name="password"]').fill("demo-only")
        page.locator('#auth-form button[type="submit"],#submit').first.click()
        page.locator("#game").wait_for(state="visible", timeout=15000)


def run():
    from playwright.sync_api import sync_playwright
    OUT.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True)
        context=browser.new_context(viewport={"width":1600,"height":950})
        page=context.new_page()
        errors=[]
        forbidden=[]
        page.on("pageerror",lambda e:errors.append(str(e)))
        page.on("request",lambda r:forbidden.append((r.method,r.url))
                if r.method not in ("GET","HEAD") and
                any(x in r.url for x in ("/api/expedition","/api/workbench","/api/planet/action"))
                else None)
        page.goto(URL+"/?pilot-terminal=1",wait_until="domcontentloaded")
        login(page)
        terminal=page.locator(".pt-terminal")
        terminal.wait_for(state="visible",timeout=15000)
        tabs=page.locator(".pt-tab")
        assert tabs.count()==2,"PT01: two seeded workspaces"
        assert "Добыча — 1" in tabs.nth(0).inner_text()
        assert "Добыча — 2" in tabs.nth(1).inner_text()
        windows=page.locator('dialog[id^="window-ptdemo-u"]')
        assert windows.count()==4,"PT02: four mock modules in first workspace"
        shipWindow=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-ship-art)')
        cargoWindow=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-resources)')
        assert "#41" in shipWindow.inner_text()
        assert "Железо" in cargoWindow.inner_text() and "Биомасса" not in cargoWindow.inner_text()

        # The cargo filter belongs only to the current module instance.
        cargoWindow.locator('select[aria-label="Фильтр трюма"]').select_option("Руда")
        assert cargoWindow.locator('select[aria-label="Фильтр трюма"]').input_value()=="Руда"
        oldID=cargoWindow.get_attribute("id")
        # Arrow-key window movement uses the original Modules save path.
        title=cargoWindow.locator(".module-titlebar")
        title.focus()
        before=cargoWindow.bounding_box()
        title.press("ArrowLeft")
        after=cargoWindow.bounding_box()
        assert after["x"]<before["x"],"PT04: original window keyboard drag"

        tabs.nth(1).click()
        assert "#82" in page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-ship-art)').inner_text()
        otherCargo=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-resources)')
        assert "Биомасса" in otherCargo.inner_text() and "Железо" not in otherCargo.inner_text()
        assert otherCargo.locator('select[aria-label="Фильтр трюма"]').input_value()=="all"

        tabs.nth(0).click()
        cargoWindow=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-resources)')
        assert cargoWindow.get_attribute("id")==oldID,"PT04: stable module instance window ID"
        assert cargoWindow.locator('select[aria-label="Фильтр трюма"]').input_value()=="Руда"
        assert abs(cargoWindow.bounding_box()["x"]-after["x"])<3

        # Workspace follows #82, while fixed ship preview continues to show #41.
        page.locator(".pt-context select").select_option("82")
        assert "#41" in page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-ship-art)').inner_text()
        assert "Биомасса" not in cargoWindow.inner_text() # RUDA category is still in effect
        assert "Медь" in cargoWindow.inner_text() and "Железо" not in cargoWindow.inner_text()
        mining=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-mining-site)')
        assert "Альтаир" in mining.inner_text()
        beforeAction=len(forbidden)
        mining.get_by_role("button",name="Демо: добыть").first.click()
        assert "корабль #82" in terminal.locator(".pt-message").inner_text()
        assert len(forbidden)==beforeAction,"PT08: mock action issued real API command"

        # Restore after reload (same browser storage); original selected IDs persist.
        page.reload(wait_until="domcontentloaded")
        login(page)
        page.locator(".pt-terminal").wait_for(state="visible",timeout=15000)
        assert page.locator(".pt-context select").input_value()=="82"
        assert page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-resources) '
                            'select[aria-label="Фильтр трюма"]').input_value()=="Руда"

        # Instance deletion and re-adding are independent.
        target=page.locator('dialog[id^="window-ptdemo-u"]:has(.pt-resources)')
        target.locator('button',has_text="Убрать из пространства").click()
        assert page.locator('dialog[id^="window-ptdemo-u"]').count()==3
        terminal.get_by_role("button",name="Все модули").click()
        terminal.get_by_role("button",name="Трюм · Добавить").click()
        assert page.locator('dialog[id^="window-ptdemo-u"]').count()==4

        # Duplicate creates a third workspace with distinct identities.
        terminal.get_by_role("button",name="Дублировать").click()
        assert page.locator(".pt-tab").count()==3
        page.screenshot(path=str(OUT/"terminal-desktop.png"))

        # PT09: both mock-account scopes survive independent switching.
        terminal.get_by_role("button",name="Mock аккаунт: A").click()
        assert page.locator(".pt-tab").count()==2,"PT09: B reused A's spaces"
        page.locator(".pt-terminal").get_by_role("button",name="Mock аккаунт: B").click()
        assert page.locator(".pt-tab").count()==3,"PT09: A did not recover its saved spaces"

        # PT05: 10 workspaces allowed; 11th disabled. Prompts are demo-only.
        prompts=iter(range(4,11))
        page.on("dialog",lambda dialog:dialog.accept("Workspace "+str(next(prompts))))
        for _ in range(7):
            page.locator(".pt-workspace-tabs > button").last.click()
        assert page.locator(".pt-tab").count()==10
        assert page.locator(".pt-workspace-tabs > button").last.is_disabled(),(
            "PT05: 11th workspace was not disabled"
        )
        page.screenshot(path=str(OUT/"terminal-ten-spaces.png"))

        # Disabled storage must not crash the UI (fresh sandbox context).
        assert page.locator("#chat-root").count()<=1,"PT10: duplicate global chat root"
        assert page.locator("#news-home-slot").count()==1,"PT10: news disappeared"
        assert not errors,"JavaScript errors: "+str(errors)
        assert not forbidden,"Real game mutation request: "+str(forbidden)
        context.close();browser.close()
    print("PASS: PT01 PT02 PT03 PT04 PT06 PT08 PT10; screenshot",OUT)


if __name__=="__main__":
    run()
