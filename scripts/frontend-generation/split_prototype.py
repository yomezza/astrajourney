"""Move previously approved research prototype v13 into proper .html/.css/.js/.svg files."""
from pathlib import Path
import json
import re

BASE = Path(__file__).resolve().parent
src=(BASE/'prototype/studies-v13.html').read_text('utf-8')
js=re.search(r'<script>(.*?)</script>',src,re.S)
css=re.search(r'<style>(.*?)</style>',src,re.S)
if not js or not css: raise RuntimeError('original prototype missing blocks')
code=js.group(1)
key='const STAR_HOLOGRAMS = '
start=code.index(key)+len(key)
obj,end=json.JSONDecoder().raw_decode(code[start:]);assert len(obj)==7
folder=BASE/'prototype/studies'; (folder/'svg').mkdir(parents=True,exist_ok=True)
for c,source in obj.items():
    (folder/'svg'/f'object-star-{c.lower()}.svg').write_text(source,'utf-8')
new='''const STAR_HOLOGRAMS = Object.create(null);
const svgReady = Promise.all(['O','B','A','F','G','K','M'].map(async code => {
  const response = await fetch('./svg/object-star-' + code.toLowerCase() + '.svg');
  if (!response.ok) throw Error('Не удалось загрузить игровую голограмму: ' + code);
  STAR_HOLOGRAMS[code] = await response.text();
})).then(() => { if(document.getElementById('studies-current')) update(); });'''
code=code[:start-len(key)]+new+code[start+end:]
# Embed mode is only a shell when mounted in Modules.create. Standalone retains entire v13 demo.
code="document.documentElement.classList.toggle('embedded', new URLSearchParams(location.search).has('embed'));\n"+code
(folder/'app.js').write_text(code,'utf-8')
styles=css.group(1)+'''
/* Only when mounted inside the actual game's Modules window, not in standalone preview. */
html.embedded body{overflow:hidden;background:var(--panel)}
html.embedded .apphead,html.embedded .sidebar,html.embedded .footer,
html.embedded .background,html.embedded .overview,html.embedded .stagehead,
html.embedded .module-titlebar{display:none!important}
html.embedded .stage{margin:0;padding:0;min-height:0}
html.embedded .module-window{position:fixed;inset:0!important;top:0!important;left:0!important;
width:100%!important;height:100dvh!important;min-height:0!important;max-height:none!important;
resize:none!important;border:0!important;border-radius:0!important;box-shadow:none!important}
html.embedded .module-content{padding:10px!important}
html.embedded .studies-content{height:100%;}
'''
(folder/'styles.css').write_text(styles,'utf-8')
html=src[:css.start()]+'<link rel="stylesheet" href="./styles.css">'+src[css.end():]
html=re.sub(r'<script>.*?</script>','<script src="./app.js"></script>',html,flags=re.S)
(folder/'index.html').write_text(html,'utf-8')
print('SPLIT',len(html),len(styles),len(code),'SVG',len(obj),'TOTAL',sum(map(len,obj.values())))
