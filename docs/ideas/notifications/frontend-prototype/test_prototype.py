#!/usr/bin/env python3
"""Optional Chromium smoke for the autonomous, file:// Astra Journey notices POC.

Run: python test_prototype.py
Requires: pip install playwright; python -m playwright install chromium
No HTTP server, game API or production services are used.
"""
from __future__ import annotations

from pathlib import Path
import os

ROOT = Path(__file__).resolve().parent
SCREENSHOTS = Path(os.environ.get('NOTIFICATION_SCREENSHOTS', str(ROOT / 'screenshots')))


def run():
    from playwright.sync_api import sync_playwright
    SCREENSHOTS.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        # System Chromium works for local screenshots and avoids extra downloads.
        opts = {'headless': True}
        if os.environ.get('CHROMIUM_PATH'):
            opts['executable_path'] = os.environ['CHROMIUM_PATH']
        browser = p.chromium.launch(**opts)
        context = browser.new_context(viewport={'width': 1440, 'height': 1060}, device_scale_factor=1)
        page = context.new_page()
        errors = []
        requests = []
        page.on('pageerror', lambda error: errors.append(str(error)))
        page.on('request', lambda req: requests.append(req.url))
        # This sandbox blocks navigation even to localhost and file://. Load
        # the exact standalone source files directly into a Chromium page.
        html=(ROOT/'index.html').read_text('utf-8')
        html=html.replace('<link rel="stylesheet" href="./styles.css">','')
        html=html.replace('<script src="./fixtures.js" defer></script>','')
        html=html.replace('<script src="./app.js" defer></script>','')
        page.set_content(html,wait_until='load')
        page.add_style_tag(content=(ROOT/'styles.css').read_text('utf-8'))
        page.add_script_tag(content=(ROOT/'fixtures.js').read_text('utf-8'))
        page.add_script_tag(content=(ROOT/'app.js').read_text('utf-8'))
        page.locator('.notice-row').first.wait_for()
        assert page.locator('.notice-row').count() == 16
        assert page.locator('#notice-count').inner_text() == '7'
        assert page.locator('.notice-window').get_attribute('data-variant') == 'a'
        assert page.locator('.notice-row[data-kind="research"]').count() > 0
        assert page.locator('.notice-row[data-kind="system"]').count() > 0
        assert page.locator('.notice-row[data-kind="economy"]').count() > 0
        assert page.locator('.notice-row[data-kind="corporation"]').count() > 0
        assert page.locator('.notice-row[data-kind="gameplay"]').count() > 0

        for variant in 'abc':
            page.locator(f'#variant-{variant}').click()
            assert page.locator('.notice-window').get_attribute('data-variant') == variant
            page.screenshot(path=str(SCREENSHOTS / f'desktop-{variant}.png'), full_page=False)
        page.locator('#variant-a').click()

        page.locator('.notice-row[data-read="false"] .notice-mark-one').first.click()
        assert page.locator('#notice-count').inner_text() == '6'
        page.locator('#filter-unread').click()
        assert page.locator('.notice-row').count() == 6
        page.locator('#mark-all').click()
        assert page.locator('.notice-row').count() == 0
        assert page.locator('.notice-empty').is_visible()
        assert page.locator('#notice-count').is_hidden()
        page.locator('#filter-all').click()
        assert page.locator('.notice-row').count() == 16

        page.locator('#new-event').click()
        assert page.locator('#notice-count').inner_text() == '1'
        page.locator('.notice-row[data-read="false"] .notice-open').first.click()
        detail = page.get_by_role('dialog', name='Новая телеметрия корабля')
        assert detail.is_visible()
        detail.get_by_role('button', name='✓ Отметить прочитанным').click()
        assert page.locator('#notice-count').is_hidden()
        page.keyboard.press('Escape')
        assert detail.is_hidden()

        page.locator('#empty-data').click()
        assert page.locator('.notice-row').count() == 0
        assert page.get_by_text('Всё спокойно').is_visible()
        page.locator('#long-data').click()
        assert page.locator('.notice-row').count() == 40
        assert page.locator('.notice-scroll').evaluate('(e)=>e.scrollHeight>e.clientHeight')
        page.locator('.notice-scroll').hover()
        page.mouse.wheel(0, 520)
        page.wait_for_timeout(100)
        assert page.locator('.notice-scroll').evaluate('(e)=>e.scrollTop') > 0
        page.locator('#reset-data').click()

        page.locator('#minimize-window').click()
        assert page.locator('.notice-window').evaluate('(e)=>e.classList.contains("is-minimized")')
        page.locator('#minimize-window').click()
        assert page.locator('.notice-scroll').is_visible()
        page.locator('#close-window').click()
        assert page.locator('.notice-window').is_hidden()
        page.locator('#reopen-window').click()
        assert page.locator('.notice-window').is_visible()
        handle = page.locator('#window-drag-handle')
        handle.focus()
        left = page.locator('.notice-window').bounding_box()['x']
        handle.press('ArrowRight')
        assert page.locator('.notice-window').bounding_box()['x'] > left

        # Native pointer drag and CSS resize handle, no alternate window manager.
        bar=handle.bounding_box()
        before=page.locator('.notice-window').bounding_box()
        page.mouse.move(bar['x']+110,bar['y']+16)
        page.mouse.down()
        page.mouse.move(bar['x']+147,bar['y']+34,steps=7)
        page.mouse.up()
        moved=page.locator('.notice-window').bounding_box()
        assert moved['x'] > before['x']+20 and moved['y'] > before['y']+10
        page.mouse.move(moved['x']+moved['width']-2,moved['y']+moved['height']-2)
        page.mouse.down()
        page.mouse.move(moved['x']+moved['width']+27,moved['y']+moved['height']+24,steps=7)
        page.mouse.up()
        resized=page.locator('.notice-window').bounding_box()
        assert resized['width']>moved['width']+15 and resized['height']>moved['height']+12

        for width, height in [(900,800),(610,850),(390,844),(320,700)]:
            page.set_viewport_size({'width': width, 'height': height})
            page.locator('#variant-c').click()
            box = page.locator('.notice-window').bounding_box()
            stage = page.locator('.preview-stage').bounding_box()
            assert box and stage and box['width'] <= stage['width'] + 1, (width, box, stage)
            assert box['x'] >= stage['x']-1, (width, box, stage)
            assert box['x']+box['width'] <= stage['x']+stage['width']+1, (width, box, stage)
            assert box['height'] <= stage['height'] + 1
            page.locator('.preview-card').scroll_into_view_if_needed()
            page.screenshot(path=str(SCREENSHOTS / f'mobile-{width}.png'), full_page=False)
        assert not errors, f'JavaScript errors: {errors}'
        assert not requests, f'Unexpected network requests: {requests}'
        browser.close()
    print('PASS: A/B/C, 5 kinds, read/unread, long list, detail, empty, resize, close, pointer drag, resize, keyboard drag, 320–1440px')
    print('Screenshot folder:', SCREENSHOTS)


if __name__ == '__main__':
    run()
