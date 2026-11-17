#!/usr/bin/env python3
"""Run every integration root test exactly once across isolated CI jobs."""
import argparse
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--index', type=int, required=True)
parser.add_argument('--count', type=int, default=4)
parser.add_argument('--list-only', action='store_true')
args = parser.parse_args()
if args.count < 1 or not 0 <= args.index < args.count:
    parser.error('index must be in [0, count)')
root = Path(__file__).resolve().parents[1]
integration = 'github.com/yomezza/cosmosimulator/internal/server/integration'
packages = subprocess.check_output(['go', 'list', './...'], cwd=root, text=True).splitlines()
if args.index == 0 and not args.list_only:
    subprocess.run(['go', 'test', *[p for p in packages if p != integration], '-count=1', '-timeout=12m'], cwd=root, check=True)
with tempfile.TemporaryDirectory(prefix='astra-ci-shard-') as temporary:
    binary = Path(temporary) / 'integration.test'
    subprocess.run(['go', 'test', '-c', '-o', str(binary), integration], cwd=root, check=True)
    names = sorted(name for name in subprocess.check_output([str(binary), '-test.list', '^Test'], text=True).splitlines() if name.startswith('Test'))
    selected = names[args.index::args.count]
    print(f'Integration shard {args.index + 1}/{args.count}: {len(selected)} of {len(names)} tests', flush=True)
    if not selected:
        raise SystemExit('empty integration shard')
    if args.list_only:
        print('\n'.join(selected))
    else:
        subprocess.run([str(binary), '-test.run', '^(' + '|'.join(selected) + ')$', '-test.timeout', '12m', '-test.v'], cwd=root / 'internal/server/integration', check=True)
