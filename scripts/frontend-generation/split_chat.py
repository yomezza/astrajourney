from pathlib import Path
import re
base=Path(__file__).resolve().parent/'prototype'
s=(base/'chat-v13.html').read_text('utf-8')
out=base/'chat';out.mkdir(exist_ok=True)
styles=re.search(r'<style>(.*?)</style>',s,re.S)
assert styles
(out/'styles.css').write_text(styles.group(1),'utf-8')
s=s[:styles.start()]+'<link rel="stylesheet" href="./styles.css">'+s[styles.end():]
js=re.findall(r'<script>(.*?)</script>',s,re.S)
assert len(js)==2
for i,script in enumerate(js):
    (out/f'{"shell" if i==0 else "chat"}.js').write_text(script,'utf-8')
i=0
def repl(m):
    global i
    i+=1
    return f'<script src="./{"shell" if i==1 else "chat"}.js"></script>'
s=re.sub(r'<script>.*?</script>',repl,s,flags=re.S)
(out/'index.html').write_text(s,'utf-8')
print('split chat',len(s),'bytes HTML', len(js),'JS files')
