#!/usr/bin/env python3
"""Run a CI check and expose test failures in the public Actions summary."""
import argparse
from pathlib import Path
import re
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--log', type=Path, required=True)
parser.add_argument('command', nargs=argparse.REMAINDER)
args = parser.parse_args()
command = args.command[1:] if args.command[:1] == ['--'] else args.command
if not command:
    parser.error('a command is required')

with args.log.open('w') as log:
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    for line in process.stdout:
        print(line, end='', flush=True)
        log.write(line)
    status = process.wait()

if status:
    lines = args.log.read_text().splitlines()
    failures = [i for i, line in enumerate(lines) if re.search(r'^not ok |--- FAIL:|^panic:', line)]
    blocks = [lines[max(0, i - 8):i + 40] for i in failures[:12]] or [lines[-80:]]
    for block in blocks:
        message = '\n'.join(block)[:14000]
        print('::error::' + message.replace('%', '%25').replace('\r', '%0D').replace('\n', '%0A'), flush=True)
raise SystemExit(status)
