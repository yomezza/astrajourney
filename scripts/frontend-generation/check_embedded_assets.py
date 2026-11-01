#!/usr/bin/env python3
"""Check that packaged exact original SVG files match pinned upstream Git blobs."""
import hashlib,json,sys
from pathlib import Path
from prepare import EXPECTED_ASSET_SHAS
BASE=Path(__file__).resolve().parent
ASSETS=json.loads((BASE/'graphics-inventory.json').read_text('utf-8'))['assets']
ROOT=BASE/'embedded-original-assets'
SHAS={**EXPECTED_ASSET_SHAS,
 'favicon.svg':'fa974b0b2334ff962fc98943f8713bc45112d891',
 'object-asteroids.svg':'b489dd7e291c57c3dfb9942c42ad5c8fa1c1f760',
 'object-black-hole.svg':'a13dfba69ffb0e9f36848d8b8dc2469ef20cf67d',
 'object-desert.svg':'67de4bf5774babc7776fbb3333ea061627777663',
 'object-garden.svg':'c8bae4824c2d37a7938dfd982320439bd60a5a7f',
 'object-gas.svg':'fcd9d89ba1bc6ccb9bdc58140578b9414fd5e968',
 'object-ice-field.svg':'0997c047685e341bae858b7af26aa17cd39d27cc',
 'object-icy.svg':'543d1c0b4d175fce2f9fbb3034e5a33eeaaf0991',
 'object-lava.svg':'7f67a9ab7c113627553ef842c792d130c049159a',
 'object-rocky.svg':'06f7572c163c64a34e09511f2f71e9e4aa0bf4ad',
 'object-star-a.svg':'210f41d41bc5d4730568c06022ea4fe9cc4b1741',
 'object-star-b.svg':'19983ad839acc7035d69c0a2b5c523204a9cc980',
 'object-star-f.svg':'38442487df1e0a4ddb2abed65080130bec8a9dd7',
 'object-star-g.svg':'49e26dc37dc7204b22b900bf7cffa63f2efc9542',
 'object-star-k.svg':'183927cbc9d015d86dc6c7854ace8e794276c4ea',
 'object-star-m.svg':'c9bd2d3ef85623430d40817dabd494be8411c1a9',
 'object-star-o.svg':'51695fce317b28b35e8e7317ef158ce237d29b44',
 'object-station.svg':'9105a53f26db013be2ac48b37af4ef3cf7e041c6',
}
assert len(SHAS)==33,len(SHAS)
fail=[]
for name,expected in sorted(SHAS.items()):
 path=ROOT/name
 if not path.exists():fail.append((name,'MISSING'));continue
 b=path.read_bytes();actual=hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()
 if actual!=expected:fail.append((name,'HASH MISMATCH '+actual))
print('33 SVG original release v0.23.0:',len(SHAS)-len(fail),'confirmed')
for f in fail:print('FAIL',*f)
if fail:sys.exit(1)
ship_names=set(ASSETS['ships']);object_names=set(ASSETS['objects'])
assert ship_names|object_names|set(ASSETS['other'])==set(SHAS)
print('15 spacecraft + 17 space objects + favicon: EXACT SHA-1 Git blob match')
