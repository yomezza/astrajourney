#!/usr/bin/env python3
"""Summarize a completed owned mixed-player experiment; optional export plot."""
import argparse
from bisect import bisect_right
from collections import defaultdict
from datetime import datetime
import json
import math
import os
from pathlib import Path
import statistics


def stamp(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


def lines(path):
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text().splitlines() if line.strip()]


def distribution(values):
    values = sorted(values)
    if not values:
        return {}
    return {'samples': len(values), 'mean': statistics.mean(values),
            'p50': values[math.ceil(len(values)*.5)-1],
            'p95': values[math.ceil(len(values)*.95)-1], 'max': values[-1]}


def counter(raw, key):
    return float(dict(line.split() for line in raw.splitlines())[key])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--plot', action='store_true', help='Requires matplotlib for an SVG export')
    args = parser.parse_args()
    directory = args.directory
    report = json.loads((directory/'report.json').read_text())
    samples = lines(directory/'samples.jsonl')
    extra = lines(directory/'process-trees.jsonl')
    times = [stamp(row['at']) for row in samples]
    groups = defaultdict(lambda: defaultdict(list))
    timeline = []
    for a, b in zip(samples, samples[1:]):
        elapsed = stamp(b['at']) - stamp(a['at'])
        if elapsed <= 0 or elapsed > 15 or a['phase'] != b['phase']:
            continue
        phase = b['phase']
        row = {'at': b['at'], 'phase': phase}
        for name, value in b['processes'].items():
            previous = a['processes'].get(name)
            if previous:
                cores = (value['cpu_seconds']-previous['cpu_seconds'])/elapsed
                if cores >= 0:
                    row[name+'_cores'] = cores
                    groups[phase][name+'_cores'].append(cores)
                groups[phase][name+'_rss_mib'].append(value['rss_bytes']/2**20)
        host = (counter(b['cgroup']['cpu.stat'], 'usage_usec')-
                counter(a['cgroup']['cpu.stat'], 'usage_usec'))/1e6/elapsed
        groups[phase]['cgroup_cores'].append(host)
        row['cgroup_cores'] = host
        row['memory_mib'] = int(b['cgroup']['memory.current'])/2**20
        groups[phase]['cgroup_memory_mib'].append(row['memory_mib'])
        for key in ['http_requests', 'http_5xx_transport', 'chat_sent', 'chat_ack_ok']:
            current = b.get('activity', {}).get(key, 0)
            previous = a.get('activity', {}).get(key, 0)
            row[key+'_per_second'] = (current-previous)/elapsed
            groups[phase][key+'_per_second'].append(row[key+'_per_second'])
        row['logs_mib'] = sum(b['logs_bytes'].values())/2**20
        for name in ['cosmos', 'planet']:
            process = b.get(name, {}).get('data', {}).get('process', {})
            prev = a.get(name, {}).get('data', {}).get('process', {})
            if 'sql_calls' in process and 'sql_calls' in prev:
                groups[phase][name+'_sql_per_second'].append((process['sql_calls']-prev['sql_calls'])/elapsed)
        diag = ((b.get('chat_diagnostics') or {}).get('metrics') or {})
        if diag:
            groups[phase]['chat_connections'].append(diag.get('connections', 0))
        planet = b.get('planet', {}).get('data', {})
        if 'connections' in planet:
            groups[phase]['planet_connections'].append(planet['connections'])
        timeline.append(row)
    supplement = []
    for a, b in zip(extra, extra[1:]):
        elapsed = stamp(b['at'])-stamp(a['at'])
        index = bisect_right(times, stamp(b['at']))-1
        if elapsed <= 0 or elapsed > 15 or index < 0:
            continue
        phase = samples[index]['phase']
        row = {'at': b['at'], 'phase': phase}
        for name, tree_name, include in [
            ('postgres', 'postgres_tree', lambda _: True),
            ('nats', 'generator_tree', lambda v: v['comm'] == 'nats-server'),
            ('browser', 'generator_tree', lambda v: v['comm'].startswith(('node', 'chrom'))),
        ]:
            current = {k: v for k, v in b[tree_name].items() if include(v)}
            previous = a[tree_name]
            cpu = sum(max(0, v['cpu_seconds']-previous[k]['cpu_seconds']) for k, v in current.items() if k in previous)
            cores = cpu/elapsed
            groups[phase][name+'_tree_cores'].append(cores)
            groups[phase][name+'_tree_sum_rss_mib'].append(sum(v['rss_bytes'] for v in current.values())/2**20)
            row[name+'_cores'] = cores
        supplement.append(row)
    summary = {
        'run': report['run'], 'phase_result': report['phase_result'],
        'units': 'CPU in cores; memory in MiB; sum RSS double counts shared pages',
        'instrumentation': 'First run /proc task/children unavailable: use supplemental postgres/nats/browser tree values. Earlier postgres_cluster and browser_probe_tree values omit children.',
        'resources': {phase: {name: distribution(values) for name, values in metrics.items()}
                      for phase, metrics in groups.items()},
        'http_phases': {}, 'browser': None,
    }
    for phase in report['phases']:
        name = phase['name']
        metrics = {k[len(name)+1:]: v for k, v in report['result']['metrics'].items()
                   if k.startswith(name+'/http ')}
        codes = defaultdict(int)
        for item in metrics.values():
            for code, count in item['codes'].items():
                codes[code] += count
        summary['http_phases'][name] = {'phase_seconds': phase['seconds'], 'codes': dict(codes),
                                      'operations': metrics,
                                      'requests_per_second': sum(codes.values())/phase['seconds']}
    browser_path = directory/'browser.json'
    if browser_path.exists():
        browser = json.loads(browser_path.read_text())
        summary['browser'] = {k: v for k, v in browser.items() if k != 'http_errors'}
        summary['browser']['recorded_http_errors_capped_at_100'] = len(browser.get('http_errors', []))
        summary['browser']['module_latency_ms'] = distribution([v['ms'] for v in browser['module_openings'] if v['ok']])
    (directory/'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2)+'\n')
    if args.plot:
        os.environ.setdefault('MPLCONFIGDIR', '/tmp/astra-mixed100-matplotlib')
        import matplotlib
        matplotlib.use('Agg')
        import matplotlib.pyplot as plt
        start = times[0]
        minutes = lambda rows: [(stamp(row['at'])-start)/60 for row in rows]
        fig, axes = plt.subplots(4, 1, figsize=(12, 12), sharex=True, constrained_layout=True)
        for name in ['cosmos', 'chat', 'web', 'planet']:
            axes[0].plot(minutes(timeline), [v.get(name+'_cores', math.nan) for v in timeline], label=name, lw=1)
        axes[0].plot(minutes(supplement), [v['postgres_cores'] for v in supplement], label='PostgreSQL tree', lw=1)
        axes[0].set(ylabel='CPU cores', title='Astra Journey 0.30.0 · mixed 100 players · all service diagnostics enabled')
        axes[0].legend(ncol=5, fontsize=8)
        axes[1].plot(minutes(timeline), [v['cgroup_cores'] for v in timeline], label='All processes, including load generator/browser', lw=1)
        axes[1].axhline(4, color='red', ls='--', label='Cgroup CPU quota: 4 cores')
        axes[1].set(ylabel='Total CPU cores', ylim=(0, None)); axes[1].legend(fontsize=8)
        axes[2].plot(minutes(timeline), [v['http_requests_per_second'] for v in timeline], label='Programmatic HTTP requests/s', lw=1)
        axes[2].set(ylabel='HTTP requests/s'); axes[2].legend(loc='upper left', fontsize=8)
        secondary = axes[2].twinx()
        secondary.plot(minutes(timeline), [v['chat_ack_ok_per_second'] for v in timeline], color='orange', label='Chat accepted messages/s', lw=1)
        secondary.set_ylabel('Chat messages/s'); secondary.legend(loc='upper right', fontsize=8)
        axes[3].plot(minutes(timeline), [v['memory_mib'] for v in timeline], label='Cgroup memory (includes page cache)', lw=1)
        axes[3].set(ylabel='Memory MiB', xlabel='Elapsed measurement time, minutes'); axes[3].legend(fontsize=8)
        for ax in axes:
            ax.grid(alpha=.2)
            seen = set()
            for row in timeline:
                if row['phase'] not in seen:
                    seen.add(row['phase']); ax.axvline((stamp(row['at'])-start)/60, color='gray', ls=':', alpha=.5)
        fig.savefig(directory/'overview.svg')
        svg = directory/'overview.svg'
        svg.write_text('\n'.join(line.rstrip() for line in svg.read_text().splitlines())+'\n')
        fig.savefig(directory/'overview.png', dpi=140)
        plt.close(fig)
    print(json.dumps({'run': summary['run'], 'phase_result': summary['phase_result'],
                      'phases': {key: value['codes'] for key, value in summary['http_phases'].items()}}, indent=2))


if __name__ == '__main__':
    main()
