#!/usr/bin/env python3
"""Produce a non-exhaustive, source-linked UI component inventory from original JS.
This scanner complements manual per-window inspection; it does not claim visual coverage.
"""
from pathlib import Path
import re,json,collections
BASE=Path(__file__).resolve().parent
WEB=BASE/'original-web'
pattern={
 'window_create':r'Modules\.create\(\s*[\'\"]([^\'\"]+)[\'\"]',
 'ui_menu':r'UI\.menu\s*\(',
 'ui_picker':r'UI\.(?:picker|choice)\s*\(',
 'ui_tabs':r'UI\.tabs\s*\(',
 'ui_hologram':r'(?:UI\.(?:hologram|updateHologram)|objectHologram)\s*\(',
 'ui_confirm':r'UI\.confirm\s*\(',
 'ui_table':r'UI\.table\s*\(',
 'canvas':r'(?:getContext\(|createElement\(\s*[\'\"]canvas[\'\"]|n\(\s*[\'\"]canvas[\'\"]|e\(\s*[\'\"]canvas[\'\"])',
 'svg_create':r'(?:createElementNS\(|document\.createElement\(\s*[\'\"]svg[\'\"])',
 'css_dynamic':r'\.classList\.(?:add|toggle|remove)\(',
 'dom_dynamic':r'(?:UI\.node\(|\bn\(|\be\(|\.replaceChildren\(|\.append\()',
}
if not (WEB/'bootstrap.js').exists():
 raise SystemExit('Сначала подготовьте оригинальный frontend: python prepare.py')
records=[]
for file in sorted(WEB.rglob('*.js')):
 rel=file.relative_to(WEB).as_posix();text=file.read_text('utf-8')
 for category,regex in pattern.items():
  matches=list(re.finditer(regex,text))
  if not matches:continue
  positions=[1+text.count('\n',0,m.start()) for m in matches]
  records.append({'source':rel,'component':category,'count':len(matches),'line_examples':positions[:16]})
report={'warning':'Static heuristics only; some components are dynamically generated via aliases or loops and require manual inspection.',
        'files_read':len(list(WEB.rglob('*.js'))),'references':records,
        'counts':dict(collections.Counter({k:sum(x['count'] for x in records if x['component']==k) for k in pattern}))}
(BASE/'UI-REGISTRY.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n','utf-8')
print('UI references:',sum(x['count'] for x in records),'from',report['files_read'],'JS files. Saved UI-REGISTRY.json')
