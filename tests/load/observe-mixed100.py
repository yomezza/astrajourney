#!/usr/bin/env python3
"""Read-only process-tree sampling, including virtual /proc without task/children.

Used as a supplemental observer for the first baseline. It never starts, signals,
or changes a service. End it with SIGINT after the load generator finishes.
"""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import signal
import time


def processes():
    result = {}
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        try:
            raw = (entry / 'stat').read_text()
            comm, tail = raw.split(' (', 1)[1].rsplit(') ', 1)
            fields = tail.split()
            result[int(entry.name)] = {
                'comm': comm, 'ppid': int(fields[1]),
                'cpu_seconds': (int(fields[11]) + int(fields[12])) / os.sysconf('SC_CLK_TCK'),
                'rss_bytes': int(fields[21]) * os.sysconf('SC_PAGE_SIZE'),
            }
        except (OSError, ValueError, IndexError):
            pass
    return result


def tree(table, root):
    selected = {root} if root in table else set()
    while True:
        added = {pid for pid, row in table.items() if row['ppid'] in selected} - selected
        if not added:
            break
        selected.update(added)
    return {str(pid): table[pid] for pid in selected}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--generator', type=int, required=True)
    parser.add_argument('--postgres', type=int, required=True)
    args = parser.parse_args()
    if not (args.directory / 'samples.jsonl').is_file():
        parser.error('Existing owned run directory required')
    table = processes()
    if not table.get(args.generator, {}).get('comm', '').startswith('integration.tes'):
        parser.error('Generator must be the running integration test')
    if table.get(args.postgres, {}).get('comm') != 'postgres':
        parser.error('PostgreSQL postmaster required')
    stopping = False

    def stop(*_):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    with (args.directory / 'process-trees.jsonl').open('x') as output:
        while not stopping:
            table = processes()
            if args.generator not in table:
                break
            row = {'at': dt.datetime.now(dt.timezone.utc).isoformat(),
                   'postgres_tree': tree(table, args.postgres),
                   'generator_tree': tree(table, args.generator)}
            output.write(json.dumps(row, separators=(',', ':')) + '\n')
            output.flush()
            for _ in range(10):
                if stopping:
                    break
                time.sleep(.5)


if __name__ == '__main__':
    main()
