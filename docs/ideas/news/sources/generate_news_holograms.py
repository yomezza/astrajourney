"""Rebuild news-hologram-preview.svg from the five editable vector component SVGs.

Only the combined documentation preview is generated. The five component files
are authored SVG source assets and are NOT modified by this script.
Uses Python 3 standard library; no game runtime dependency.
"""
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
ORDER = (
    "news-projection-beams.svg",
    "news-panel-left.svg",
    "news-panel-right.svg",
    "news-all-button.svg",
    "news-projector-base.svg",
)
START = '''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="560" viewBox="0 0 900 560" fill="none" role="img" aria-labelledby="np-preview-title np-preview-desc"><title id="np-preview-title">Голографический проектор новостей Cosmosimulator</title><desc id="np-preview-desc">Три световых потока от проекционного устройства: две новости и центральная кнопка «Все новости». Примерные новости только в этом превью.</desc>
'''
END = '''
<g font-family="Segoe UI,Arial,sans-serif" aria-hidden="true">
<g fill="#c7faf4" text-anchor="start">
<text x="102" y="231" font-size="11" letter-spacing="2.4" fill="#a1d7ca">ИССЛЕДОВАНИЯ</text>
<text x="102" y="255" font-size="16" font-weight="600">Открыта новая</text>
<text x="105" y="276" font-size="16" font-weight="600">звёздная система</text>
<text x="110" y="301" font-size="11" opacity=".73">Академия Эдема сообщает</text>
<text x="111" y="316" font-size="11" opacity=".73">об успешном сканировании.</text>
<text x="118" y="344" font-size="10" fill="#9bbdbf">15 минут назад</text>
</g>
<g fill="#d1f3f5" text-anchor="start">
<text x="599" y="231" font-size="11" letter-spacing="2" fill="#efc78f">ЭКОНОМИКА</text>
<text x="599" y="255" font-size="16" font-weight="600">Рынок реагирует</text>
<text x="599" y="276" font-size="16" font-weight="600">на новые поставки</text>
<text x="595" y="301" font-size="11" opacity=".73">Изменились объёмы</text>
<text x="593" y="316" font-size="11" opacity=".73">ресурсов на станциях.</text>
<text x="593" y="344" font-size="10" fill="#a9b6c5">42 минуты назад</text>
</g>
<text x="450" y="263" font-size="16" font-weight="650" text-anchor="middle" fill="#d5fff2">Все новости</text>
<text x="450" y="281" font-size="8.5" letter-spacing="2" text-anchor="middle" fill="#96d6d6" opacity=".9">NEWS FEED</text>
</g>
'''
def component(name: str) -> str:
    source = (ROOT / name).read_text(encoding="utf-8")
    begin = "<!-- news-component:begin -->"
    end = "<!-- news-component:end -->"
    if source.count(begin) != 1 or source.count(end) != 1:
        raise ValueError(f"Missing component markers: {name}")
    ET.fromstring(source)
    return source.split(begin, 1)[1].split(end, 1)[0].strip()

def main() -> None:
    chunks = [f"<!-- {name} -->\n{component(name)}" for name in ORDER]
    result = START + "\n".join(chunks) + END + "</svg>\n"
    ET.fromstring(result)
    dest = ROOT / "news-hologram-preview.svg"
    dest.write_text(result, encoding="utf-8")
    print(dest)

if __name__ == "__main__":
    main()
