"""Local browser acceptance test. Requires playwright + Chromium locally."""
from pathlib import Path
from playwright.sync_api import sync_playwright

BASE = Path(__file__).resolve().parent

def verify():
    errors=[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True, executable_path='/usr/bin/chromium', args=['--no-sandbox','--disable-web-security'])
        page=browser.new_page(viewport={"width":1440,"height":900},device_scale_factor=1)
        page.on('pageerror',lambda e: errors.append(str(e)))
        # Chromium in this environment blocks file:// by policy; evaluate exactly
        # the same four local files in a memory-backed document, without network.
        html=(BASE/'index.html').read_text(encoding='utf-8')
        html=html.replace('<link rel="stylesheet" href="./styles.css">', '<style>'+ (BASE/'styles.css').read_text(encoding='utf-8')+'</style>')
        for filename in ['fixtures.js','hologram.js','app.js']:
            html=html.replace('<script defer src="./'+filename+'"></script>', '')
        page.set_content(html, wait_until='load')
        for filename in ['fixtures.js','hologram.js','app.js']:
            page.add_script_tag(content=(BASE/filename).read_text(encoding='utf-8'))
        page.locator('.mode-c').wait_for()
        assert page.locator('.result-row').count()==25
        assert page.locator('.guide-window').count()==1
        assert page.locator('.c-main').count()==1
        page.locator('.result-row').filter(has_text='Садовый мир').first.click()
        assert page.locator('.holo-svg').count()==1
        yaw=page.locator('.holo-svg').get_attribute('data-yaw')
        assert yaw=='360-full-cycle'
        first=page.locator('.holo-svg path').first.get_attribute('d')
        page.wait_for_timeout(320)
        second=page.locator('.holo-svg path').first.get_attribute('d')
        assert first!=second,'SVG must move over time'
        page.locator('.c-big-search input').fill('несуществующее_слово_0000')
        assert page.locator('.result-empty').count()>0
        page.locator('.c-big-search input').fill('')
        page.locator('.c-tabs button').filter(has_text='Избранное').click()
        assert page.locator('.result-empty').count()>0
        page.locator('.article-actions button').filter(has_text='избранное').first.click()
        assert page.locator('.result-row').count()==1
        page.locator('.c-tabs button').filter(has_text='Недавние').click()
        assert page.locator('.result-row').count()>=1
        page.locator('.c-tabs button').filter(has_text='Все темы').click()
        page.locator('#stress').check()
        assert page.locator('.result-row').count()==65
        page.locator('.c-index').evaluate('(e)=>e.scrollTop=150')
        before=page.locator('.c-index').evaluate('(e)=>e.scrollTop')
        assert before>0,before
        page.locator('.result-row').nth(8).evaluate('(e)=>e.click()')
        after=page.locator('.c-index').evaluate('(e)=>e.scrollTop')
        assert abs(after-before)<2,(before,after)
        page.locator('#preset').select_option('compact')
        assert page.locator('.guide-window').get_attribute('data-preset')=='compact'
        page.locator('#empty').check()
        assert page.locator('.result-empty').count()>0
        page.locator('#reset').click()
        assert page.locator('.result-row').count()==25
        for size in [320,390,810,1440]:
            page.set_viewport_size({'width':size,'height':800})
            page.wait_for_timeout(80)
            overflow=page.evaluate('document.documentElement.scrollWidth>window.innerWidth+2')
            assert not overflow,('horizontal page overflow',size)
        page.set_viewport_size({'width':1440,'height':900})
        page.locator('.result-row').filter(has_text='Газовый гигант').first.click()
        page.screenshot(path=str(BASE/'original-c-preview.png'),full_page=True)
        assert not errors,errors
        browser.close()
    print('PASS: 25 articles, search, tabs, favorites, animated SVG, 65-row scroll, empty, reset, sizes, 0 errors')

if __name__=='__main__': verify()