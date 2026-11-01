#!/usr/bin/env python3
"""Stable entrypoint: update source (optional), prepare original files and run local UI."""
import argparse
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def main():
    p = argparse.ArgumentParser(description="Cosmosimulator frontend mirror launcher")
    p.add_argument("--offline", action="store_true", help="no GitHub requests; use files already installed")
    p.add_argument("--no-update", action="store_true", help="skip updates of demo program")
    p.add_argument("--no-prepare", action="store_true", help="skip installation of original frontend")
    args = p.parse_args()
    if not args.no_update:
        cmd = [sys.executable, str(ROOT / "update.py")]
        if args.offline: cmd.append("--offline")
        # Always continue with the currently installed, tested version on failure.
        subprocess.run(cmd, cwd=ROOT, check=False)
    if not args.no_prepare:
        cmd = [sys.executable, str(ROOT / "prepare.py")]
        if args.offline:
            cmd.append("--check-only")
        result = subprocess.run(cmd, cwd=ROOT, check=False)
        if result.returncode:
            print("[демо] Не удалось подготовить оригинальный web. Если он уже установлен, проверка доступна отдельно; запускаю локальный сервер.")
    return subprocess.call([sys.executable, str(ROOT / "server.py")], cwd=ROOT)


if __name__ == "__main__":
    raise SystemExit(main())
