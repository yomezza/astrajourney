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
        print('[демо] Проверка обновлений программы перед запуском…', flush=True)
        cmd = [sys.executable, str(ROOT / 'update.py'), '--strict']
        if args.offline: cmd.append('--offline')
        result = subprocess.run(cmd, cwd=ROOT, check=False)
        if result.returncode:
            print('[демо] ВНИМАНИЕ: автоматическое обновление НЕ прошло! '
                  'Демоверсия запускается из локальных файлов. '
                  'Запустите python update.py вручную и проверьте сообщения выше.',
                  flush=True)
    else:
        print('[демо] Обновления пропущены по параметру --no-update.', flush=True)
    # Read the state after the updater exits; never invent an installed commit.
    from update import local_version
    info = local_version()
    print('[демо] Установленный коммит: ' + (info['commit'] or 'НЕИЗВЕСТЕН') + (' [проверен]' if info['verified'] else ' [не подтверждён]'), flush=True)
    if not info['verified']:
        print('[демо] ' + info['message'] + '. Выполните python update.py для повторной проверки.', flush=True)
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
