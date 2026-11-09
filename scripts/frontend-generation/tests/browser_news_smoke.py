#!/usr/bin/env python3
"""Optional real Chromium integration smoke test for the local news hologram.

Requires the original-web assets and a running frontend-generation server:
    python launch.py --offline
    pip install playwright
    python -m playwright install chromium
    python tests/browser_news_smoke.py

No production API is used; all test traffic targets the localhost demo.
"""
import argparse
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit, parse_qsl, urlencode

VIEWPORTS = [(1920,1080),(1440,900),(1280,800),(1024,768),
             (881,800),(880,760),(760,820),(390,844)]


def assert_true(value, message):
    if not value:
        raise AssertionError(message)


def with_case(base, name):
    uri=urlsplit(base)
    params=dict(parse_qsl(uri.query))
    params['news_case']=name
    return urlunsplit((uri.scheme,uri.netloc,'/',urlencode(params),''))


def login_if_necessary(page):
    page.wait_for_function(
        """() => {
            const game=document.getElementById('game');
            const auth=document.getElementById('auth');
            return (game&&!game.hidden)||(auth&&!auth.hidden);
        }""", timeout=15000)
    if page.locator("#auth").is_visible():
        page.locator('#auth-form input[name="username"]').fill('demo_pilot')
        page.locator('#auth-form input[name="password"]').fill('test_password')
        page.locator('#auth-form button[type="submit"], #submit').first.click()
        page.locator('#game').wait_for(state='visible',timeout=15000)


def wait_news(page):
    page.locator("#news-home-slot .news-hologram-widget").wait_for(
        state='visible', timeout=15000)
    page.wait_for_function(
        """() => {
          const root=document.querySelector('.news-hologram-widget');
          return root && root.querySelector('.news-story-heading')?.textContent
            && root.querySelectorAll('.news-layer').length===5;
        }""", timeout=15000)


def check_images(page):
    return page.locator('.news-hologram-widget .news-layer').evaluate_all(
        "(imgs) => imgs.every(img => img.complete && img.naturalWidth > 0)")


def snapshot(page):
    return page.locator('.news-hologram-widget .news-float-track').evaluate_all(
        """(panels) => panels.map(node => ({
          label:node.getAttribute('aria-label'),
          title:node.querySelector('.news-story-heading')?.textContent,
          category:node.querySelector('.news-story-kind')?.textContent,
          summary:node.querySelector('.news-story-desc')?.textContent
        }))""")


def check_overlap(page):
    return page.evaluate("""() => {
      const a=document.querySelector('#chat-home-slot .chat-root');
      const b=document.querySelector('#news-home-slot .news-stage');
      if(!a||!b)return false;
      const x=a.getBoundingClientRect(),y=b.getBoundingClientRect();
      return Math.min(x.right,y.right)>Math.max(x.left,y.left)+2 &&
             Math.min(x.bottom,y.bottom)>Math.max(x.top,y.top)+2;
    }""")


def run(base, screenshot_dir):
    try:
        from playwright.sync_api import sync_playwright
    except ImportError as e:
        raise SystemExit('Нужен Playwright: pip install playwright; python -m playwright install chromium') from e
    screenshot_dir.mkdir(parents=True,exist_ok=True)
    with sync_playwright() as playwright:
        browser=playwright.chromium.launch(headless=True)
        try:
            page=browser.new_page(viewport={'width':1440,'height':900})
            errors=[]
            page.on('pageerror',lambda error:errors.append(str(error)))
            page.goto(with_case(base,'normal'),wait_until='domcontentloaded',timeout=20000)
            login_if_necessary(page)
            wait_news(page)
            page.wait_for_function("""() => [...document.querySelectorAll(
              '.news-hologram-widget .news-layer')].every(img => img.complete)""",timeout=12000)
            assert_true(check_images(page),'Одна или несколько оригинальных SVG не загрузились')
            select=page.locator('#demo-news-variant')
            assert_true(select.count()==1,'Дубликат переключателя новостей')
            before=snapshot(page)
            assert_true(before[0]['title'] and before[1]['title'],
                        'Две последние новости не появились')
            assert_true(page.locator('#chat-home-slot').is_hidden(),
                        'Чат неожиданно закрепился сам')
            assert_true(page.locator('#news-home-slot').is_visible(),
                        'Новости недоступны при незакреплённом чате')

            # Real CSS treatments, same DOM and same story content on every choice.
            for value in ('a','b','c','a'):
                select.select_option(value)
                assert_true(page.locator('.news-hologram-widget').get_attribute('data-variant')==value,
                            'Не переключился вариант '+value)
                assert_true(snapshot(page)==before,'При смене варианта поменялись новости')
                assert_true(page.locator('.news-hologram-widget').count()==1,
                            'Появились копии проектора')
                assert_true(page.locator('#chat-home-slot').is_hidden(),
                            'Переключение новостей изменило режим чата')
            assert_true(page.evaluate(
                "() => JSON.parse(localStorage.getItem('cosmosim-demo-news-variant-v1'))")=='a',
                'Выбор A не сохранился')

            page.reload(wait_until='domcontentloaded')
            login_if_necessary(page)
            wait_news(page)
            assert_true(page.locator('#demo-news-variant').input_value()=='a',
                        'Вариант не восстановился после обновления')

            # The official single Modules instance, not iframe.
            page.locator('.news-all-news').click()
            page.locator('#window-news-all-demo').wait_for(state='visible',timeout=6000)
            assert_true(page.locator('#window-news-all-demo .news-list-item').count()==12,
                        'В окне «Все новости» не отображается полный каталог')
            assert_true(page.locator('#window-news-all-demo iframe').count()==0,
                        'Окно новостей не должно использовать iframe')

            page.evaluate("""async () => {
                const module=await import('/core/i18n.js');
                module.I18n.set('en');
            }""")
            page.locator('#demo-news-variant option[value="c"]').wait_for(state='attached')
            page.wait_for_function("""() => document.querySelector(
                '#demo-news-variant option[value="c"]')?.textContent.includes('Volumetric')""")
            assert_true(page.locator('.news-all-news').get_attribute('aria-label')=='All news',
                        'RU/EN не переключилось')

            # Independently verify the two-column and stacked dock layouts.
            page.evaluate("""async () => {
                const module=await import('/core/i18n.js');
                module.I18n.set('ru');
            }""")
            page.locator('[data-chat-open]').click()
            page.locator('#chat-dock-toggle').click()
            page.locator('#chat-home-slot .chat-root').wait_for(state='visible')
            assert_true(not check_overlap(page),'Закреплённый чат перекрывает новости')

            for width,height in VIEWPORTS:
                page.set_viewport_size({'width':width,'height':height})
                news=page.locator('#news-home-slot')
                news.scroll_into_view_if_needed(timeout=6000)
                assert_true(news.is_visible(),f'Новости скрыты при {width}x{height}')
                assert_true(not check_overlap(page),f'Перекрытие чата при {width}x{height}')
                box=news.bounding_box()
                stage=page.locator('#news-home-slot .news-stage').bounding_box()
                assert_true(box is not None and stage is not None and stage['width']>240,
                            f'Голограмма обрезалась при {width}x{height}')
                assert_true(check_images(page),f'SVG не загружаются при {width}x{height}')
                page.screenshot(path=str(screenshot_dir/f'news-{width}x{height}.png'),full_page=True)
                print(f'OK {width}x{height}: projector {stage["width"]:.0f}px')

            page.locator('#chat-dock-toggle').click()
            assert_true(page.locator('#news-home-slot').is_visible(),
                        'При откреплении чата исчезли новости')
            page.close()

            # Test explicit stress fixtures and graceful error behavior.
            for name, count in [('long',2),('nodesc',2),('one',1),('empty',0),('error',0)]:
                check=browser.new_page(viewport={'width':520,'height':800})
                check.goto(with_case(base,name),wait_until='domcontentloaded')
                login_if_necessary(check)
                wait_news(check)
                if name=='error':
                    check.get_by_text('Не удалось загрузить тестовые новости.').wait_for()
                    assert_true(check.locator('.news-retry').is_visible(),'Кнопка Retry скрыта')
                elif name=='empty':
                    check.get_by_text('В тестовом источнике нет новостей.').wait_for()
                else:
                    assert_true(bool(check.locator('.news-story-heading').first.text_content()),
                                'Нет читаемого заголовка')
                check.screenshot(path=str(screenshot_dir/f'news-case-{name}.png'),full_page=True)
                check.close()
                print('OK scenario '+name)
            assert_true(not errors,'JS errors: '+'; '.join(errors))
            print('PASS: 5 exact SVGs, 3 variants, persistence, chat layout, news window, RU/EN, 8 viewports')
        finally:
            browser.close()


if __name__ == '__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--url',default='http://127.0.0.1:8765/')
    parser.add_argument('--screenshots',default='news-smoke-screenshots')
    args=parser.parse_args()
    run(args.url,Path(args.screenshots))
