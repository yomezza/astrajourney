#!/usr/bin/env python3
"""No Go services or database: original frontend + fictitious API responses."""
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
from urllib.parse import urlparse, parse_qs, unquote
from copy import deepcopy
from datetime import datetime, timezone
import json
import mimetypes
import os
import sys

ROOT=Path(__file__).resolve().parent
WEB=ROOT/'original-web'
sys.path.insert(0,str(ROOT/'mock'))
import data as fixture

HOST='127.0.0.1'; PORT=int(os.getenv('PORT','8765'))

class DemoHandler(SimpleHTTPRequestHandler):
    def respond(self,obj,status=200):
        body=json.dumps(obj,ensure_ascii=False).encode('utf-8')
        self.send_response(status)
        self.send_header('Content-Type','application/json; charset=utf-8')
        self.send_header('Content-Length',str(len(body)))
        self.send_header('Cache-Control','no-store')
        self.end_headers()
        self.wfile.write(body)
    def route_api(self,route,query,body):
        # The client does not implement a second authorization system.
        # All mutations here only change RAM; nothing is persisted or sent.
        if route=='system':
            try: index=int(query.get('id',[1])[0])-1
            except (ValueError,TypeError): index=0
            if index not in range(len(fixture.STARS)): return self.respond({'error':'Нет такой системы'},404)
            star=fixture.STARS[index]
            sites=fixture.SITES if index==0 else [{**s,'id':s['id']+index*10,'name':s['name'].replace('Эдем',star['name'])} for s in fixture.SITES[:3]]
            return self.respond({'star':star,'sites':sites,'region':fixture.REGIONS[star['region']-1]['name'],'surveyed':True,'notes':[],'ports':[]})
        if route=='estimate':
            data=fixture.api_data(route).copy()
            try: to=int(query.get('to',[1])[0])
            except (ValueError,TypeError):to=1
            data['path']=[fixture.SHIP['star'],to] if to!=fixture.SHIP['star'] else [to]
            data['cost']=10+(to!=fixture.SHIP['star'])*15
            data['travel_seconds']=0 if to==fixture.SHIP['star'] else 40
            return self.respond(data)
        if route.startswith('login') or route=='logout' or route=='register':
            return self.respond({'ok':True})
        if body is not None:
            if route=='workbench':
                action=body.get('action')
                if action=='pause_research':
                    for j in fixture.RESEARCH:
                        if j['ship']==body.get('ship'): j['paused']=True
                if action=='resume_research':
                    for j in fixture.RESEARCH:
                        if j['ship']==body.get('ship'):j['paused']=False
                if action=='explore':
                    target=int(body.get('to',1))
                    fixture.RESEARCH.append({'ship':13,'star':target,'scanned':0,'total':6,'seconds':2,'paused':False})
                return self.respond({'ok':True})
            if route=='notifications': return self.respond({'ok':True})
            if route=='profile/upgrade':
                fixture.PROFILE['level']+=1
                fixture.PROFILE['xp']=0
                fixture.USER['credits']-=fixture.PROFILE['upgrade_cost']
                return self.respond({'ok':True})
            if route=='expedition':return self.respond({'ok':True})
            # Mock writable routes acknowledge with generic in-memory demo response.
            return self.respond({'ok':True,'items':[]})
        result=fixture.api_data(route)
        if result is not None:return self.respond(result)
        # Explicit dummy, not silent access to any real game infrastructure.
        return self.respond({'items':[],'total':0,'count':0,'status':'demo'},200)

    def do_POST(self):
        url=urlparse(self.path)
        if not url.path.startswith('/api/'):
            return self.respond({'error':'Not found'},404)
        try:
            size=int(self.headers.get('Content-Length','0'))
            if size>100000:return self.respond({'error':'Payload too large'},413)
            body=json.loads(self.rfile.read(size) or '{}')
        except (ValueError,TypeError,UnicodeDecodeError):
            return self.respond({'error':'Malformed JSON'},400)
        return self.route_api(url.path[5:],parse_qs(url.query),body)

    def do_GET(self):
        url=urlparse(self.path)
        if url.path.startswith('/api/'):
            return self.route_api(url.path[5:],parse_qs(url.query),None)
        if url.path=='/demo/assets.json':
            return self.respond(json.loads((ROOT/'graphics-inventory.json').read_text('utf-8')))
        if url.path in ('/demo/assets','/demo/assets/'):
            url=url._replace(path='/mock/assets/index.html')
        if url.path=='/':url=url._replace(path='/index.html')
        path=url.path
        if path=='/index.html':
            if not (WEB/'index.html').is_file():
                html='''<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Cosmosimulator UI Mirror — подготовка</title><style>body{background:#10151b;color:#dce5ed;font:14px/1.6 system-ui;max-width:780px;margin:10vh auto;padding:24px}h1{font-size:23px;color:#efad58}code,pre{background:#1b2b35;padding:4px 6px}pre{padding:14px;overflow:auto}a{color:#78c7d9}</style><h1>◈ Cosmosimulator UI Mirror</h1><p>Для запуска всех оригинальных модулей сначала скопируйте 152 файла frontend из релиза v0.23.0.</p><pre>python prepare.py
python server.py</pre><p>Если GitHub недоступен: <code>python prepare.py --from ПУТЬ_К_РЕПОЗИТОРИЮ</code> или <code>python prepare.py --archive source.zip</code>.</p><p>Все 33 оригинальных SVG уже включены в архив и доступны для проверки:</p><p><a href="/demo/assets/">Открыть галерею SVG кораблей, звёзд и космических объектов</a></p><p>До загрузки оригинала картинки PNG отмечаются как отсутствующие. Работающая игра не имитируется.</p></html>'''
                encoded=html.encode('utf-8')
                self.send_response(200);self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Content-Length',str(len(encoded)));self.end_headers()
                return self.wfile.write(encoded)
            index=(WEB/'index.html').read_text('utf-8')
            marker='<script type="module" src="bootstrap.js"></script>'
            if marker not in index:
                return self.respond({'error':'Original bootstrap marker not found'},500)
            index=index.replace(marker,marker+'\n    <link rel="stylesheet" href="/mock/ui-overlay.css">\n    <link rel="stylesheet" href="/mock/chat-native.css">\n    <script type="module" src="/mock/ui-overlay.js"></script>')
            encoded=index.encode('utf-8')
            self.send_response(200); self.send_header('Content-Type','text/html; charset=utf-8')
            self.send_header('Content-Length',str(len(encoded)));self.send_header('Cache-Control','no-store');self.end_headers()
            return self.wfile.write(encoded)
        for mount,base in [('/mock/',ROOT/'mock'),('/prototype/',ROOT/'prototype'),('/',WEB)]:
            if path.startswith(mount):
                relative=unquote(path[len(mount):]).lstrip('/')
                target=(base/relative).resolve()
                if not target.is_relative_to(base.resolve()):return self.send_error(403)
                if target.is_dir():target=target/'index.html'
                if not target.is_file():
                    # Exact-release fallback for all 33 SVG models already
                    # bundled in the ZIP (identical original Git blob hashes).
                    fallback=(ROOT/'embedded-original-assets'/relative).resolve() if mount=='/' else None
                    if fallback and fallback.is_file():target=fallback
                    else:return self.send_error(404,'Asset not found')
                encoded=target.read_bytes()
                self.send_response(200)
                self.send_header('Content-Type',(mimetypes.guess_type(target.name)[0] or 'application/octet-stream')+('; charset=utf-8' if target.suffix in ['.js','.css','.html','.svg','.json'] else ''))
                self.send_header('Content-Length',str(len(encoded)))
                self.end_headers()
                return self.wfile.write(encoded)
        return self.send_error(404)

if __name__=='__main__':
    if not (WEB/'bootstrap.js').is_file():
        print('ВНИМАНИЕ: original-web ещё не подготовлен. Сейчас доступна галерея всех 33 SVG; для полной игры выполните python prepare.py.')
    server=ThreadingHTTPServer((HOST,PORT),DemoHandler)
    print('Cosmosimulator / оригинальный frontend + вымышленные данные')
    print(f'Откройте http://{HOST}:{PORT}/')
    print('Сервер слушает только localhost. Для остановки Ctrl+C.')
    try:server.serve_forever()
    except KeyboardInterrupt:print('\nОстановка сервера.')
    finally:server.server_close()
