#!/usr/bin/python3
"""恢复从面板接入的固定普通规则，保留所有其他链。"""
import fcntl
import json
import pathlib
import shlex
import subprocess

CONFIG = pathlib.Path('/etc/opsd/firewall/inherited.json')

def run(args, content=None):
    return subprocess.run(args, input=content, text=True, capture_output=True, check=True, timeout=30).stdout

def restore(command, saved):
    text = run([command + '-save', '-t', 'filter'])
    lines = text.splitlines()
    existing = {line.split()[0][1:] for line in lines if line.startswith(':')}
    changes = []
    for chain, expected in saved['chains'].items():
        if not chain.startswith('1PANEL_') or any(shlex.split(rule)[:2] != ['-A', chain] for rule in expected):
            raise RuntimeError('接入配置链无效')
        actual = [line for line in lines if line.startswith('-A ' + chain + ' ')]
        if actual == expected and chain in existing:
            continue
        if actual:
            raise RuntimeError('已有面板规则不同，请核对：' + chain)
        if chain not in existing:
            changes.append(':' + chain + ' - [0:0]')
        changes.extend(expected)
    references = saved['references']
    for parent, expected in references.items():
        if parent not in ['INPUT', 'FORWARD', 'OUTPUT']:
            raise RuntimeError('规则接入链无效')
        actual = [line for line in lines if line.startswith('-A ' + parent + ' ')]
        panel = [line for line in actual if any(target in saved['chains'] for option, target in zip(shlex.split(line), shlex.split(line)[1:]) if option in ['-j', '-g'])]
        if panel == expected:
            continue
        if panel:
            raise RuntimeError('面板入站引用不同，请核对：' + parent)
        # 原普通链位于 Galera 规则之后；OPSD 互信引用继续放在最前。
        position = 0
        for i, line in enumerate(actual):
            if '-j CODEX_GALERA' in line or 'opsd-peer-trust' in line:
                position = i + 1
        for offset, rule in enumerate(expected):
            args = shlex.split(rule)
            if args[:2] != ['-A', parent]:
                raise RuntimeError('面板引用内容无效')
            changes.append(shlex.join(['-I', parent, str(position + offset + 1)] + args[2:]))
    if changes:
        body = '*filter\n' + '\n'.join(changes) + '\nCOMMIT\n'
        run([command + '-restore', '--wait', '10', '--noflush', '--test'], body)
        run([command + '-restore', '--wait', '10', '--noflush'], body)
    # 读回固定链和引用，不能用进程成功代替实际结果。
    after = run([command + '-save', '-t', 'filter']).splitlines()
    for chain, expected in saved['chains'].items():
        if [line for line in after if line.startswith('-A ' + chain + ' ')] != expected:
            raise RuntimeError('普通规则恢复后不一致：' + chain)
    for parent, expected in references.items():
        actual = [line for line in after if line.startswith('-A ' + parent + ' ')]
        panel = [line for line in actual if any(target in saved['chains'] for option, target in zip(shlex.split(line), shlex.split(line)[1:]) if option in ['-j', '-g'])]
        if panel != expected:
            raise RuntimeError('普通规则引用恢复后不一致：' + parent)
    return len(changes)

if __name__ == '__main__':
    with open('/run/lock/opsd-inherited-firewall.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        config = json.loads(CONFIG.read_text())
        result = {command: restore(command, saved) for command, saved in config.items()}
        print(json.dumps({'restored': True, 'changed': result}))
