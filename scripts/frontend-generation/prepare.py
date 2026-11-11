#!/usr/bin/env python3
"""Copy/download a pinned and complete Astra Journey web tree; validate every asset."""
import argparse, hashlib, io, json, shutil, sys, urllib.request, zipfile, time
from pathlib import Path
BASE=Path(__file__).resolve().parent
TARGET=BASE/'original-web'
MANIFEST=json.loads((BASE/'graphics-inventory.json').read_text('utf-8'))
REVISION=MANIFEST['source_commit']
ARCHIVES=[
    f'https://codeload.github.com/yomezza/cosmosimulator/zip/{REVISION}',
    f'https://github.com/yomezza/cosmosimulator/archive/{REVISION}.zip',
]
PREFIX='internal/server/web/'

# Display only during actual copying/downloading, not during normal server use.
class Progress:
    def __init__(self, title, total=0, unit='файлов'):
        self.title, self.total, self.unit, self.start, self.last = title, max(0,total), unit, time.monotonic(), 0
        self.draw(0, force=True)
    def draw(self, value, force=False):
        now=time.monotonic()
        if not force and now-self.last < 0.11 and value < self.total:return
        self.last=now
        amount=max(0,int(value))
        ratio=min(1,amount/self.total) if self.total else 0
        filled=round(ratio*26)
        bar='['+'#'*filled+'-'*(26-filled)+']'
        percentage=f'{ratio*100:5.1f}%' if self.total else '  ... '
        if self.unit=='байт':
            progress=f'{amount/1048576:.1f} / {self.total/1048576:.1f} MiB' if self.total else f'{amount/1048576:.1f} MiB'
        else:progress=f'{amount} / {self.total}'
        print(f'\r{self.title}: {bar} {percentage} {progress} {self.unit}  ',end='',flush=True)
    def done(self, value):
        self.draw(value,force=True)
        print(f'  ({time.monotonic()-self.start:.1f} c)')

def download_with_progress(url):
    request=urllib.request.Request(url,headers={'User-Agent':'astra-journey-ui-mirror-v3'})
    with urllib.request.urlopen(request,timeout=70) as response:
        total=int(response.headers.get('Content-Length') or 0)
        progress=Progress('Загрузка оригинала',total,'байт')
        dest=BASE/'.source-download.zip'
        count=0
        try:
            with dest.open('wb') as fp:
                while True:
                    chunk=response.read(262144)
                    if not chunk:break
                    fp.write(chunk)
                    count+=len(chunk)
                    progress.draw(count)
            progress.done(count)
            return dest
        except BaseException:
            dest.unlink(missing_ok=True)
            print()
            raise

def from_local_with_progress(source):
    files=[p for p in source.rglob('*') if p.is_file()]
    progress=Progress('Копирование оригинала',len(files))
    for i,file in enumerate(files,1):
        out=(TARGET/file.relative_to(source))
        out.parent.mkdir(parents=True,exist_ok=True)
        shutil.copy2(file,out)
        progress.draw(i)
    progress.done(len(files))
REQUIRED=['index.html','bootstrap.js','ru.json','en.json','styles/tokens.css','styles/terminal.css','ui/windows.js','ui/holograms.js','features/research/index.js']
REQUIRED += [name for group in MANIFEST['assets'].values() for name in group]

def from_tree(folder):
    p=Path(folder).expanduser().resolve()
    if (p/'bootstrap.js').is_file() and (p/'styles/terminal.css').is_file(): return p
    if (p/PREFIX/'bootstrap.js').is_file(): return p/PREFIX
    raise ValueError(f'Не найден каталог исходного frontend: {p} (ожидался {PREFIX})')

def from_archive(archive):
    output=TARGET.resolve()
    with zipfile.ZipFile(archive) as z:
        entries=[x for x in z.infolist() if not x.is_dir() and '/'+PREFIX in x.filename and x.filename.split('/'+PREFIX,1)[1]]
        progress=Progress('Распаковка web-клиента',len(entries))
        copied=0
        for item in entries:
            rel=item.filename.split('/'+PREFIX,1)[1]
            target=(TARGET/rel).resolve()
            if not target.is_relative_to(output):raise ValueError('Недопустимый путь в ZIP-архиве')
            target.parent.mkdir(parents=True,exist_ok=True)
            with z.open(item) as f, target.open('wb') as sink:shutil.copyfileobj(f,sink)
            copied+=1
            progress.draw(copied)
        progress.done(copied)
        if copied < MANIFEST['expected_original_web_files']:
            raise ValueError(f'ZIP неполный: {copied} из {MANIFEST["expected_original_web_files"]} файлов')

def git_blob_sha(path):
    content=path.read_bytes()
    return hashlib.sha1(b'blob '+str(len(content)).encode()+b'\0'+content).hexdigest()

def validate():
    missing=[p for p in REQUIRED if not (TARGET/p).is_file()]
    files=[p for p in TARGET.rglob('*') if p.is_file()]
    sprites=[p for p in files if p.suffix.lower() in ('.svg','.png')]
    found={category:sum((TARGET/p).is_file() for p in values) for category,values in MANIFEST['assets'].items()}
    size=sum(p.stat().st_size for p in files)
    # Commit-level check: exact expected original contents, not only file names.
    deviations=[]
    for path,sha in EXPECTED_ASSET_SHAS.items():
        file=TARGET/path
        if file.exists() and git_blob_sha(file)!=sha:deviations.append(path)
    report={'release':MANIFEST['release'],'reference_commit':REVISION,'files':len(files),'bytes':size,
            'original_expected_files':MANIFEST['expected_original_web_files'],'original_expected_bytes':MANIFEST['expected_original_web_size'],
            'assets':found,'missing_assets':missing,'asset_sha_mismatch':deviations,
            'ok':len(files)==MANIFEST['expected_original_web_files'] and size==MANIFEST['expected_original_web_size'] and not missing and not deviations}
    (BASE/'inventory-result.json').write_text(json.dumps(report,indent=2,ensure_ascii=False)+'\n','utf-8')
    return report

EXPECTED_ASSET_SHAS={
    'pilgrim.svg':'974d1cdd42d46542089d4ad2df7ca92928cc5467',
    'argus.svg':'770754b4bdfefa255cbb2537b7bb9a4ebfd2e780',
    'ark.svg':'f149a11e1d621a07bc825b7cb827122f5dffef57',
    'astra.svg':'45225bf3797b46e11ca46f79296decca3e8f0682',
    'atlas.svg':'432873c98f2a463911c9a86cc0b18d08f915cac7',
    'crucible.svg':'256275ee27ee52a8d7b83f241e9914e118dcd5b1',
    'forge.svg':'4f3511b0d8ac5df7e4c9727e118fe486af0d17be',
    'hermes.svg':'ef609a9e32618ed610ae5995f49720a65377240c',
    'iris.svg':'0575582f8d8f95a2dcff18b55aabd79699643b8b',
    'leviathan.svg':'494f8bb044452521b47670398028522af9deb87e',
    'lightning.svg':'3a362a6e1d957ad9c05c86890e0a6f318f0a3b0e',
    'titan.svg':'d6faf2c244868618882cd4288716a5d44fcbc82f',
    'vesper.svg':'3116e0e82e262d783f649504a936c2c28047f279',
    'wanderer.svg':'3c2da7a7ec6be74e4e587259f69c8916957192c0',
    'zephyr.svg':'52a00e38703be56a838e7e61cda06e4fd95322cd',
}

def main():
    p=argparse.ArgumentParser(description='Установить оригинальный frontend + SVG кораблей/объектов + PNG планет')
    p.add_argument('--from',dest='local',help='локальный checkout Astra Journey либо его internal/server/web/')
    p.add_argument('--archive',type=Path,help='заранее скачанный ZIP исходников с GitHub')
    p.add_argument('--check-only',action='store_true',help='проверить полноту, ничего не загружая')
    p.add_argument('--force',action='store_true')
    args=p.parse_args()
    checkout=BASE.parents[1]/'internal'/'server'/'web'
    if not args.check_only and (args.force or not (TARGET/'bootstrap.js').is_file()):
        if TARGET.exists(): shutil.rmtree(TARGET)
        TARGET.mkdir(parents=True,exist_ok=True)
        if args.local:from_local_with_progress(from_tree(args.local))
        elif args.archive:from_archive(args.archive)
        elif checkout.joinpath('bootstrap.js').is_file():
            print('Найден оригинальный frontend рядом с scripts/frontend-generation: копирование без сети.')
            from_local_with_progress(checkout)
        else:
            errors=[]
            for url in ARCHIVES:
                try:
                    path=download_with_progress(url)
                    try:from_archive(path)
                    finally:path.unlink(missing_ok=True)
                    break
                except Exception as e:errors.append(str(e))
            else:raise SystemExit('GitHub недоступен. Скачайте ZIP релиза или используйте --from ПУТЬ / --archive ПУТЬ.zip.\n'+'\n'.join(errors))
    report=validate()
    print('Оригинальный frontend:',report['files'],'файлов /',report['bytes'],'байт')
    for group,count in report['assets'].items():print(' •',group,count,'из',len(MANIFEST['assets'][group]))
    if not report['ok']:
        print('ОШИБКА: содержимое frontend не соответствует полному оригиналу v0.23.0.')
        if report['missing_assets']:print('Отсутствуют:',', '.join(report['missing_assets']))
        if report['asset_sha_mismatch']:print('Изменённые SVG кораблей:',', '.join(report['asset_sha_mismatch']))
        print('Подробности: inventory-result.json')
        raise SystemExit(2)
    print('Подтверждено: все 152 файла, включая 15 кораблей, 17 космических объектов, 25 PNG, CSS, JS, HTML и локализации.')
    # Duplicate only assets needed by standalone demos. All bytes come from source.
    demo_svg=BASE/'prototype'/'studies'/'svg'
    demo_svg.mkdir(parents=True, exist_ok=True)
    for colour in 'obafgkm':
        name=f'object-star-{colour}.svg'
        shutil.copy2(TARGET/name, demo_svg/name)
    embedded=BASE/'embedded-original-assets'
    embedded.mkdir(parents=True, exist_ok=True)
    for asset in [name for group in MANIFEST['assets'].values() for name in group if name.endswith('.svg')]:
        shutil.copy2(TARGET/asset, embedded/Path(asset).name)
    print('SVG для автономного каталога и голограмм синхронизированы с original-web.')

if __name__=='__main__':main()
