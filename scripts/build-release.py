#!/usr/bin/env python3
"""Build versioned amd64 release binaries; Linux by default."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SERVICES = {
    "cosmos": ("astra-journey", "internal/server/app.go"),
    "planet": ("astra-journey-planet", "internal/planetserver/app.go"),
    "chat": ("astra-journey-chat", "internal/chatserver/server.go"),
}


def capture(*args, cwd=ROOT):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def verify_binary(path, platform):
    with path.open("rb") as stream:
        header = stream.read(64)
        if platform == "windows":
            if header[:2] != b"MZ":
                raise ValueError("expected a Windows PE executable")
            stream.seek(struct.unpack_from("<I", header, 0x3C)[0])
            pe = stream.read(6)
            if pe[:4] != b"PE\0\0" or struct.unpack_from("<H", pe, 4)[0] != 0x8664:
                raise ValueError("expected a Windows amd64 executable")
        else:
            if header[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", header, 18)[0] != 62:
                raise ValueError("expected a Linux x86-64 ELF executable")
            offset = struct.unpack_from("<Q", header, 32)[0]
            size, count = struct.unpack_from("<HH", header, 54)
            for index in range(count):
                stream.seek(offset + index * size)
                if struct.unpack("<I", stream.read(4))[0] in (2, 3):
                    raise ValueError("Linux release must not require a dynamic loader")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", help="vMAJOR.MINOR.PATCH")
    parser.add_argument("--platform", choices=["both", "linux", "windows"], default="linux")
    parser.add_argument("--service", choices=list(SERVICES), action="append")
    parser.add_argument("--source-ref", help="build an exact Git commit/tag instead of the working tree")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.tag):
        parser.error("tag must be vMAJOR.MINOR.PATCH")
    services = list(dict.fromkeys(args.service or SERVICES))
    platforms = ["windows", "linux"] if args.platform == "both" else [args.platform]
    output = ROOT / "downloads"
    output.mkdir(exist_ok=True)
    targets = [(service, platform, output / f"{SERVICES[service][0]}-{args.tag}-{platform}-amd64{'.exe' if platform == 'windows' else ''}")
               for service in services for platform in platforms]
    for _, _, path in targets:
        if path.exists():
            parser.error(f"refusing to overwrite {path.name}; choose a new version or select only a missing platform/service")
    source_commit = capture("git", "rev-parse", "--verify", f"{args.source_ref or 'HEAD'}^{{commit}}")
    dirty = not args.source_ref and bool(capture("git", "status", "--porcelain", "--", "cmd", "internal", "go.mod", "go.sum"))
    manifest_path = output / f"astra-journey-{args.tag}-build.json"
    manifest = json.loads(manifest_path.read_text()) if manifest_path.exists() else {"tag": args.tag, "builds": []}
    additions = []
    with tempfile.TemporaryDirectory(prefix="cosmos-release-") as directory:
        temporary = Path(directory)
        source = ROOT
        if args.source_ref:
            archive = temporary / "source.tar"
            with archive.open("wb") as stream:
                subprocess.run(["git", "archive", source_commit, "cmd", "internal", "go.mod", "go.sum"], cwd=ROOT, stdout=stream, check=True)
            source = temporary / "source"
            source.mkdir()
            with tarfile.open(archive) as stream:
                stream.extractall(source, filter="data")
        for service in services:
            version = re.search(r'const Version = "([^"]+)"', (source / SERVICES[service][1]).read_text())
            if not version or version[1] != args.tag[1:]:
                parser.error(f"{service} source version does not match {args.tag}")
        if "cosmos" in services:
            subprocess.run(["go", "run", "./cmd/astra-guide-assets", "-check"], cwd=source, check=True)
        compiler = capture("go", "version", cwd=source)
        for service, platform, target in targets:
            environment = os.environ.copy()
            environment.update(GOOS=platform, GOARCH="amd64", GOAMD64="v1", CGO_ENABLED="0")
            binary = temporary / target.name
            subprocess.run(["go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/" + SERVICES[service][0]], cwd=source, env=environment, check=True)
            verify_binary(binary, platform)
            # Copy through the destination filesystem, then rename atomically.
            staged = target.with_suffix(target.suffix + ".partial")
            try:
                staged.write_bytes(binary.read_bytes())
                staged.chmod(0o755)
                staged.replace(target)
            finally:
                staged.unlink(missing_ok=True)
            digest = hashlib.sha256(target.read_bytes()).hexdigest()
            additions.append({"file": target.name, "service": service, "goos": platform, "goarch": "amd64", "goamd64": "v1", "cgo_enabled": False, "source_ref": args.source_ref, "source_commit": source_commit, "source_dirty": dirty, "compiler": compiler, "sha256": digest})
            print(f"{target.name}  {digest}", flush=True)
    manifest["builds"].extend(additions)
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    sums = output / "SHA256SUMS"
    paths = [output / item["file"] for item in additions] + [manifest_path]
    replace = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}
    lines = sums.read_text().splitlines() if sums.exists() else []
    lines = [line for line in lines if line.split(maxsplit=1)[-1].strip() not in replace]
    lines.extend(f"{digest}  {path}" for path, digest in replace.items())
    sums.write_text("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
