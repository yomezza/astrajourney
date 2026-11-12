#!/usr/bin/env python3
"""Offline Chromium smoke for all 5 homepage designs; no game-service requests."""
from pathlib import Path
import os
import re
from playwright.sync_api import sync_playwright

ROOT=Path(__file__).resolve().parent
SCREEN=ROOT/'screenshots'
SCREEN.mkdir(exist_ok=True)

def main():
    with sync_playwright() as playwright:
        browser=playwright.chromium.launch(executable_path=os.getenv('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
        page=browser.new_page(viewport={'width':1440,'height':900}, device_scale_factor=1)
        errors=[]
        requests=[]
        page.on('pageerror',lambda exc: errors.append(str(exc)))
        page.on('request',lambda req: requests.append(req.url) if req.url.startswith(('http:', 'https:', 'ws:','wss:')) and '127.0.0.1' not in req.url else None)
        # Test exact local source files through in-memory navigation; the sandbox blocks file:// and localhost.
        html=(ROOT/'index.html').read_text()
        html=re.sub(r'<link[^>]+href="styles.css"[^>]*>', '', html)
        html=re.sub(r'<script[^>]+src="(?:fixtures|app)\.js"[^>]*></script>', '', html)
        page.set_content(html)
        page.add_style_tag(content=(ROOT/'styles.css').read_text())
        page.add_script_tag(content=(ROOT/'fixtures.js').read_text())
        page.add_script_tag(content=(ROOT/'app.js').read_text())
        page.locator('.design-a').wait_for()
        assert page.locator('[data-bind="pilot"]').first.inner_text()=='ASTER-07'
        assert page.locator('.telemetry').count()==3
        assert page.locator('[data-bind="credits"]').first.inner_text()!='—'
        assert page.locator('.mock-window').count()==0
        variants={ 'a':'Mission Control','b':'Minimal','c':'Personal','d':'Holographic','e':'Modular'}
        for v,name in variants.items():
            page.locator(f'.variant-selector [data-variant="{v}"]').click()
            assert page.locator(f'.design-{v}').count()==1,(v,'missing variant')
            assert page.locator('.telemetry').count()==3,(v,'missing telemetry')
            assert page.locator('[data-bind="pilot"]').first.inner_text()=='ASTER-07',v
            page.screenshot(path=str(SCREEN/f'variant-{v}.png'),full_page=True)
        # 3 window states: empty / galaxy / galaxy+ship+chat
        for scene,num in [('empty',0),('one',1),('multi',3)]:
            page.locator(f'.demo-scenarios [data-scene="{scene}"]').click()
            assert page.locator('.mock-window').count()==num,(scene,num)
            assert page.locator(f'.demo-scenarios [data-scene="{scene}"]').get_attribute('aria-pressed')=='true'
            page.screenshot(path=str(SCREEN/f'variant-e-{scene}-windows.png'))
        # Open/focus from buttons in five variants, retain window state on switching.
        page.locator('.variant-selector [data-variant="a"]').click()
        assert page.locator('.mock-window').count()==3
        page.locator('.design-a [data-open="studies"]').first.click()
        assert page.locator('.mock-window').count()==4
        page.locator('.design-a [data-open="studies"]').first.click()
        assert page.locator('.mock-window').count()==4
        # Window controls: pin, minimize, restore, close.
        study=page.locator('.mock-window[data-module="studies"]')
        study.locator('.window-pin').click()
        assert study.locator('.window-pin').get_attribute('aria-pressed')=='true'
        study.locator('.window-min').click()
        assert 'minimized' in study.get_attribute('class')
        study.locator('.window-min').click()
        assert 'minimized' not in study.get_attribute('class')
        study.locator('.window-close').click()
        assert page.locator('.mock-window').count()==3
        # Keyboard window move
        galaxy=page.locator('.mock-window[data-module="galaxy"]')
        old=galaxy.evaluate('(e)=>parseFloat(e.style.left)')
        galaxy.locator('.window-titlebar').focus()
        page.keyboard.press('ArrowRight')
        assert galaxy.evaluate('(e)=>parseFloat(e.style.left)') == old + 10
        # Mouse drag repositions the window independently of the homepage.
        header=galaxy.locator('.window-titlebar')
        rect=header.bounding_box()
        start_left=galaxy.evaluate('(e)=>parseFloat(e.style.left)')
        page.mouse.move(rect['x']+58,rect['y']+17)
        page.mouse.down()
        page.mouse.move(rect['x']+83,rect['y']+38,steps=5)
        page.mouse.up()
        assert galaxy.evaluate('(e)=>parseFloat(e.style.left)') >= start_left+10
        # Reset demo to empty, then click module opening from another variant
        page.locator('.demo-scenarios [data-scene="empty"]').click()
        page.locator('.variant-selector [data-variant="b"]').click()
        page.locator('.minimal-links [data-open="galaxy"]').click()
        assert page.locator('.mock-window').count()==1
        page.locator('.demo-scenarios [data-scene="empty"]').click()
        # Mock credit/XP upgrade, refresh
        initial=page.locator('[data-bind="level"]').first.inner_text()
        page.locator('[data-action="upgrade"]').first.click()
        assert int(page.locator('[data-bind="level"]').first.inner_text())==int(initial)+1
        assert page.locator('[data-bind="credits"]').first.inner_text() != '184 250'
        page.locator('#demo-refresh').click()
        assert page.locator('[data-bind="updated"]').first.inner_text().startswith('Данные обновлены:')
        # Validate keyboard variant switching
        page.locator('.variant-selector [data-variant="c"]').focus()
        page.keyboard.press('ArrowRight')
        assert page.locator('.design-d').count()==1
        # Viewport checks across all five modes
        for width,height in [(1920,1080),(1440,900),(1366,768),(1280,720),(390,844),(320,650)]:
            page.set_viewport_size({'width':width,'height':height})
            for v in variants:
                page.locator(f'.variant-selector [data-variant="{v}"]').click()
                measure=page.evaluate('''() => ({scroll:document.documentElement.scrollWidth,view:innerWidth,body:document.body.scrollWidth})''')
                assert measure['scroll']<=measure['view']+1,(width,height,v,measure)
                assert page.locator('.telemetry').count()==3,(width,v)
                assert page.locator('[data-bind="system"]').first.inner_text().startswith('Эдем'),(width,v)
                assert page.locator('[data-bind="date"]').first.inner_text().startswith('ЭД'),(width,v)
                if width == 390:
                    page.screenshot(path=str(SCREEN/f'mobile-{v}.png'),full_page=True)
        # A small viewport must keep newly opened windows entirely on screen.
        page.set_viewport_size({'width':390,'height':844})
        page.locator('.demo-scenarios [data-scene="multi"]').click()
        for box in page.locator('.mock-window').all():
            r=box.bounding_box()
            assert r['x'] >= -1 and r['x'] + r['width'] <=391,(r,'mobile window overflow')
        page.locator('.demo-scenarios [data-scene="empty"]').click()
        assert not errors,errors
        assert not requests,requests
        browser.close()
        print('PASS: five variants, telemetry, upgrades, local refresh, 0/1/3 windows, pin/minimize/close, keyboard, six viewports, no external requests or JS errors')

if __name__=='__main__':main()