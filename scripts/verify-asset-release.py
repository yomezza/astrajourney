#!/usr/bin/env python3
"""Verify the public pack linked from a release, without running game services."""
import hashlib
import json
from pathlib import Path
import sys
import zipfile

root = Path(__file__).resolve().parents[1]
release = json.loads((root / 'downloads/astra-journey-assets.json').read_text())
if release['tag'] != sys.argv[1] or release['schema_version'] != 1:
    raise SystemExit('graphics release tag/schema mismatch')
archive = root / 'downloads' / release['archive']
raw = archive.read_bytes()
if len(raw) != release['size'] or hashlib.sha256(raw).hexdigest() != release['sha256']:
    raise SystemExit('graphics archive checksum mismatch')
with zipfile.ZipFile(archive) as pack:
    manifest = json.loads(pack.read('manifest.json'))
    if manifest['revision'] != release['revision']:
        raise SystemExit('graphics revision mismatch')
    for entry in manifest['entries']:
        body = pack.read(entry['path'])
        if len(body) != entry['size'] or hashlib.sha256(body).hexdigest() != entry['sha256']:
            raise SystemExit(f"corrupt graphics entry: {entry['path']}")
print(f"Verified {len(manifest['entries'])} external resources")
