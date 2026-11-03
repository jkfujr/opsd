"""由 opsd_remote.py 通过 SSH 标准输入执行，不保存凭据。"""

import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys

SERVICES = ("opsd-executor.service", "opsd.service")


def run(args):
    result = subprocess.run(args, capture_output=True, text=True, encoding="utf-8", errors="replace")
    if result.returncode:
        raise RuntimeError("执行失败：" + args[0] + " " + result.stderr.strip())
    return result.stdout.strip()


def load_config(path):
    config = json.loads(Path(path).read_text(encoding="utf-8"))
    for key in ("binary", "data_dir", "executor_dir"):
        value = config.get(key, "")
        if not value or not Path(value).is_absolute():
            raise ValueError("实例配置缺少绝对路径：" + key)
    return config


def database(config, key, name):
    path = Path(config[key]) / name
    return sqlite3.connect(path.as_uri() + "?mode=ro", uri=True, timeout=3)


def pending(config):
    counts = {}
    for key, name, table in (("data_dir", "node.db", "queue"), ("executor_dir", "executor.db", "operations")):
        with closing(database(config, key, name)) as db:
            counts[table] = db.execute("SELECT count(*) FROM " + table + " WHERE terminal=0").fetchone()[0]
    return counts


def require_idle(config):
    if any(pending(config).values()):
        raise RuntimeError("存在未结束操作，未替换程序")


def inspect(config):
    with closing(database(config, "data_dir", "node.db")) as db:
        node = json.loads(db.execute("SELECT config FROM node_identity WHERE id=1").fetchone()[0])
    return {"paths": {key: config[key] for key in ("binary", "data_dir", "executor_dir")},
            "node": {key: node.get(key) for key in ("name", "address", "region", "console", "revision")},
            "unfinished": pending(config), "version": run([config["binary"], "version"]),
            "services": run(["systemctl", "show", *SERVICES, "-p", "Id", "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "ExecMainStartTimestamp"])}


def update(config, candidate):
    target = Path(config["binary"])
    source = Path(candidate)
    if not source.is_absolute() or not source.is_file() or not target.is_file() or source.is_symlink() or target.is_symlink() or source.resolve() == target.resolve():
        raise ValueError("候选程序路径无效或与当前程序相同")
    require_idle(config)
    os.chmod(source, 0o755)
    version = run([str(source), "version"])
    print(json.dumps({"candidate_version": version}, ensure_ascii=False), flush=True)
    try:
        run(["systemctl", "stop", "opsd.service", "opsd-executor.service"])
        require_idle(config)
        os.replace(source, target)
    finally:
        run(["systemctl", "start", *SERVICES])
    run(["systemctl", "is-active", *SERVICES])
    return inspect(config)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--action", choices=("inspect", "update"), default="inspect")
    parser.add_argument("--config", required=True)
    parser.add_argument("--candidate")
    args = parser.parse_args(argv)
    try:
        config = load_config(args.config)
        result = inspect(config) if args.action == "inspect" else update(config, args.candidate or "")
        print(json.dumps(result, ensure_ascii=False, indent=2), flush=True)
        return 0
    except (OSError, ValueError, RuntimeError, sqlite3.Error, TypeError) as error:
        print("操作未完成：" + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
