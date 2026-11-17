#!/usr/bin/env python3
"""Check an external Web package before publishing; never runs a service."""
import argparse
import hashlib
import json
from pathlib import Path
import zipfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("tag")
parser.add_argument("--directory", type=Path, default=Path(__file__).resolve().parents[1] / "downloads")
args = parser.parse_args()
release = json.loads((args.directory / "astra-journey-web.json").read_text())
if release["tag"] != args.tag or release["schema_version"] != 1 or release["client_contract"] != "astra-web-v1" or release["api_contract"] != "astra-api-v1":
    raise SystemExit("Web tag/contract mismatch")
archive = args.directory / release["archive"]
raw = archive.read_bytes()
if len(raw) != release["size"] or hashlib.sha256(raw).hexdigest() != release["sha256"]:
    raise SystemExit("Web archive checksum/size mismatch")
with zipfile.ZipFile(archive) as pack:
    manifest = json.loads(pack.read("manifest.json"))
    if manifest["revision"] != release["revision"] or manifest["api_contract"] != release["api_contract"]:
        raise SystemExit("Web revision/contract mismatch")
    if len(pack.namelist()) != len(manifest["entries"]) + 1:
        raise SystemExit("unexpected Web archive members")
    for entry in manifest["entries"]:
        body = pack.read(entry["path"])
        if len(body) != entry["size"] or hashlib.sha256(body).hexdigest() != entry["sha256"]:
            raise SystemExit("corrupt Web entry: " + entry["path"])
print(f"Verified {len(manifest['entries'])} Web files; graphics stay in CDN")
