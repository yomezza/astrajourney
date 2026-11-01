#!/usr/bin/env python3
"""Update this demonstration's tracked source files from one verified GitHub commit.

This updates scripts/frontend-generation only, never the original game, database or
user data. Source files downloaded from GitHub are checked against Git blob SHAs.
"""
import argparse
import ast
import hashlib
import json
import os
import shutil
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parent
REPO = "yomezza/cosmosimulator"
PREFIX = "scripts/frontend-generation/"
API = "https://api.github.com/repos/" + REPO
RAW = "https://raw.githubusercontent.com/" + REPO
STATE = ROOT / ".mirror-update-state.json"
ALLOWED = {".py", ".js", ".json", ".css", ".html", ".md", ".bat", ".sh", ".svg", ".txt"}
IGNORE_DIRS = {"original-web", "__pycache__", ".git", "node_modules", ".mirror-backup", "tmp", "local-data"}
MAX_FILES = 350
MAX_TOTAL = 25_000_000
TIMEOUT = 18
RETRIES = 3


class UpdateError(RuntimeError):
    pass


def git_sha(payload):
    return hashlib.sha1(b"blob " + str(len(payload)).encode("ascii") + b"\x00" + payload).hexdigest()


def request(url, *, timeout=TIMEOUT):
    req = urllib.request.Request(url, headers={
        'User-Agent': 'Cosmosimulator-Frontend-Mirror/1',
        'Accept': 'application/vnd.github+json',
    })
    for attempt in range(RETRIES):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as res:
                return res.read()
        except urllib.error.HTTPError as error:
            if error.code not in (429, 500, 502, 503, 504) or attempt + 1 == RETRIES:
                raise
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            if attempt + 1 == RETRIES:
                raise
        time.sleep(0.75 * (attempt + 1))


def remote_manifest(fetch=request, ref="main"):
    """Return immutable commit and filtered files. No untrusted host or path is accepted."""
    branch = json.loads(fetch(f"{API}/branches/{ref}").decode("utf-8"))
    commit = branch["commit"]["sha"]
    if len(commit) != 40 or any(c not in "0123456789abcdef" for c in commit):
        raise UpdateError("GitHub вернул некорректный идентификатор коммита")
    tree = json.loads(fetch(f"{API}/git/trees/{commit}?recursive=1").decode("utf-8"))
    if tree.get("truncated"):
        raise UpdateError("Неполное дерево файлов GitHub: обновление отменено")
    sources = {}
    total = 0
    for item in tree.get("tree", []):
        path = item.get("path", "")
        if item.get("type") != "blob" or not path.startswith(PREFIX):
            continue
        relative = path[len(PREFIX):]
        pure = PurePosixPath(relative)
        if (not relative or pure.is_absolute() or ".." in pure.parts or
                any(part in IGNORE_DIRS or part.startswith(".") for part in pure.parts) or
                pure.suffix.lower() not in ALLOWED):
            continue
        size, sha = item.get("size", 0), item.get("sha", "")
        if not isinstance(size, int) or size < 0 or size > MAX_TOTAL or len(sha) != 40:
            raise UpdateError("Недопустимый файл в списке обновлений: " + relative)
        sources[relative] = (sha, size)
        total += size
        if len(sources) > MAX_FILES or total > MAX_TOTAL:
            raise UpdateError("Объём обновления превышает ограничения")
    if not sources or "update.py" not in sources or "README.md" not in sources:
        raise UpdateError("В GitHub не найден полный каталог программы; ничего не изменено")
    return commit, sources


def load_state():
    try:
        data = json.loads(STATE.read_text("utf-8"))
        return data if isinstance(data, dict) else {}
    except (FileNotFoundError, ValueError, OSError):
        return {}


def changed_files(files):
    result = []
    for relative, (expected_sha, _) in files.items():
        target = ROOT.joinpath(*PurePosixPath(relative).parts)
        if not target.is_file() or git_sha(target.read_bytes()) != expected_sha:
            result.append(relative)
    return result


def save_state(commit, files):
    """Persist the checked commit, even when no files needed downloading."""
    payload = {
        "commit": commit,
        "checked_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "files": len(files),
        "file_shas": {name: sha for name, (sha, _) in files.items()},
    }
    staged = STATE.with_name(STATE.name + '.tmp')
    staged.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    os.replace(staged, STATE)


def local_version():
    state = load_state()
    commit = state.get('commit', '')
    if not isinstance(commit, str) or len(commit) != 40 or any(c not in '0123456789abcdef' for c in commit):
        return {'commit': None, 'verified': False, 'files': 0, 'message': 'Коммит демоверсии неизвестен: обновление ещё не завершено'}
    sha_map = state.get('file_shas')
    if not isinstance(sha_map, dict) or not sha_map:
        return {'commit': commit, 'verified': False, 'files': state.get('files', 0), 'message': 'Необходима повторная проверка исходников'}
    missing = []
    for name, expected in sha_map.items():
        target = ROOT.joinpath(*PurePosixPath(name).parts)
        if (not target.is_file() or target.is_symlink()
                or git_sha(target.read_bytes()) != expected):
            missing.append(name)
    return {'commit': commit, 'verified': not missing, 'files': len(sha_map), 'changed': missing[:10],
            'message': 'Исходники проверены' if not missing else 'Локальные файлы не совпадают с записанным коммитом'}


def update(*, check_only=False, offline=False, fetch=request, announce=print):
    if offline:
        announce("[обновление] Проверка отключена: локальный режим.")
        return 0
    commit, files = remote_manifest(fetch)
    changed = changed_files(files)
    if not changed:
        if not check_only:
            save_state(commit, files)
        announce("[обновление] Исходники актуальны. Установленный коммит: " + commit + ".")
        return 0
    total = sum(files[name][1] for name in changed)
    announce(f"[обновление] Найдено {len(changed)} изменений ({total / 1048576:.2f} MiB), коммит {commit[:12]}.")
    if check_only:
        for name in changed: announce("   • " + name)
        return len(changed)
    # Stage and verify ALL bytes before touching the running installation.
    with tempfile.TemporaryDirectory(prefix=".mirror-staging-", dir=ROOT) as tmp:
        staged = Path(tmp)
        sofar = 0
        for index, name in enumerate(changed, 1):
            address = RAW + "/" + commit + "/" + PREFIX + name
            data = fetch(address)
            sha, size = files[name]
            if len(data) != size or git_sha(data) != sha:
                raise UpdateError(f"Контрольная сумма не совпадает: {name}")
            if name.endswith('.py'):
                try:
                    ast.parse(data.decode('utf-8'), filename=name)
                except (SyntaxError, UnicodeDecodeError) as error:
                    raise UpdateError(f"Ошибка синтаксиса в обновлении {name}: {error}") from error
            dst = staged.joinpath(*PurePosixPath(name).parts)
            dst.parent.mkdir(parents=True, exist_ok=True)
            dst.write_bytes(data)
            sofar += len(data)
            width = 24
            filled = round(width * index / len(changed))
            announce(f"[обновление] [{'#' * filled}{'-' * (width - filled)}] {index}/{len(changed)} · {sofar / 1048576:.2f}/{total / 1048576:.2f} MiB · {name}")
        # Keep a full backup of overwritten files, including local edits.
        backup = ROOT / ".mirror-backup" / (time.strftime("%Y%m%d-%H%M%S") + "-" + commit[:8])
        backups = []
        applied = []
        try:
            for name in changed:
                dst = ROOT.joinpath(*PurePosixPath(name).parts)
                if dst.is_symlink():
                    raise UpdateError("Символическая ссылка вместо исходника: " + name)
                dst.parent.mkdir(parents=True, exist_ok=True)
                if dst.exists():
                    copy = backup.joinpath(*PurePosixPath(name).parts)
                    copy.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(dst, copy)
                    backups.append((dst, copy))
                os.replace(staged.joinpath(*PurePosixPath(name).parts), dst)
                applied.append(dst)
        except Exception:
            for dst in reversed(applied):
                copy = next((source for old, source in backups if old == dst), None)
                if copy: shutil.copy2(copy, dst)
                else: dst.unlink(missing_ok=True)
            raise
        save_state(commit, files)
        announce("[обновление] Установленный коммит: " + commit)
        announce("[обновление] Предыдущие версии изменённых файлов: " + str(backup.relative_to(ROOT)) + "/")
    return len(changed)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Проверить обновления frontend-демоверсии в GitHub")
    parser.add_argument("--check", action="store_true", help="только показать изменения, ничего не скачивать")
    parser.add_argument("--offline", action="store_true", help="не обращаться к GitHub")
    parser.add_argument("--strict", action="store_true", help="при ошибке сети возвращать ненулевой код")
    args = parser.parse_args(argv)
    try:
        update(check_only=args.check, offline=args.offline)
    except (OSError, ValueError, KeyError, UpdateError, urllib.error.URLError) as error:
        print("[обновление] Не удалось проверить GitHub: " + str(error), file=sys.stderr)
        print("[обновление] Локальные исходники остаются доступными.", file=sys.stderr)
        return 2 if args.strict else 0
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
