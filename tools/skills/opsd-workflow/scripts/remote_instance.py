"""由 opsd_remote.py 通过 SSH 标准输入执行，不保存凭据。"""

import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import time

SERVICES = ("opsd-executor.service", "opsd.service")
MIN_ROOT_FREE = 2 * 1024**3


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


def require_root_space():
    root = os.statvfs("/")
    available = root.f_bavail * root.f_frsize
    if available < MIN_ROOT_FREE:
        raise RuntimeError(f"根分区可用空间为 {available / 1024**3:.2f} GiB，至少需要 2 GiB，未停服")


def inspect(config):
    with closing(database(config, "data_dir", "node.db")) as db:
        node = json.loads(db.execute("SELECT config FROM node_identity WHERE id=1").fetchone()[0])
    return {"paths": {key: config[key] for key in ("binary", "data_dir", "executor_dir")},
            "node": {key: node.get(key) for key in ("name", "address", "region", "console", "revision")},
            "unfinished": pending(config), "version": run([config["binary"], "version"]),
            "services": run(["systemctl", "show", *SERVICES, "-p", "Id", "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "ExecMainStartTimestamp"])}


def update(config, config_path, candidate, required):
    target = Path(config["binary"])
    source = Path(candidate)
    if not source.is_absolute() or not source.is_file() or not target.is_file() or source.is_symlink() or target.is_symlink() or source.resolve() == target.resolve():
        raise ValueError("候选程序路径无效或与当前程序相同")
    require_root_space()
    require_idle(config)
    os.chmod(source, 0o755)
    version = run([str(source), "version"])
    capability_check(source, config_path, required)
    print(json.dumps({"candidate_version": version}, ensure_ascii=False), flush=True)
    require_root_space()
    try:
        run(["systemctl", "stop", "opsd.service", "opsd-executor.service"])
        require_idle(config)
        os.replace(source, target)
    finally:
        run(["systemctl", "start", *SERVICES])
    run(["systemctl", "is-active", *SERVICES])
    return inspect(config)


def capability_check(binary, path, required):
    if not required or not Path(required).is_absolute():
        raise ValueError("更新需要绝对路径的授权清单")
    result = run([str(binary), "capability", "check", "--config", str(path), "--require", required])
    print(result, flush=True)


def config_candidate(path, body, metadata):
    with tempfile.NamedTemporaryFile(prefix=".instance-", dir=path.parent, delete=False) as staged:
        try:
            os.fchown(staged.fileno(), metadata.st_uid, metadata.st_gid)
            os.fchmod(staged.fileno(), metadata.st_mode & 0o777)
            staged.write(body)
            staged.flush()
            os.fsync(staged.fileno())
        except Exception:
            Path(staged.name).unlink(missing_ok=True)
            raise
    return Path(staged.name)


def grants(config, config_path, candidate, required, backup):
    path = Path(config_path)
    source = Path(candidate)
    if not backup or not all(p.is_absolute() for p in (path, source, Path(backup))) or path.is_symlink() or not path.is_file():
        raise ValueError("授权更新需要普通实例配置、候选授权表和备份目录的绝对路径")
    before = path.read_bytes()
    if json.loads(before) != config:
        raise RuntimeError("实例配置已变化，请重新读取")
    next_config = {**config, "source_capabilities": json.loads(source.read_bytes())}
    next_body = (json.dumps(next_config, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    metadata = path.stat()
    staged = config_candidate(path, next_body, metadata)
    try:
        capability_check(config["binary"], staged, required)
        require_idle(config)
        keep = Path(backup)
        keep.mkdir(mode=0o700)
        (keep / "instance.before.json").write_bytes(before)
        (keep / "instance.before.json").chmod(0o600)
        replaced = False
        since = int(time.time())
        try:
            run(["systemctl", "stop", "opsd.service"])
            require_idle(config)
            if path.read_bytes() != before:
                raise RuntimeError("实例配置已变化，请重新读取")
            os.replace(staged, path)
            replaced = True
            run(["systemctl", "start", "opsd.service"])
            run(["systemctl", "is-active", *SERVICES])
            observed = inspect(next_config)
            (keep / "instance.after.json").write_bytes(path.read_bytes())
            (keep / "instance.after.json").chmod(0o600)
        except Exception:
            if replaced:
                run(["systemctl", "stop", "opsd.service"])
                os.replace(config_candidate(path, before, metadata), path)
            run(["systemctl", "start", "opsd.service"])
            run(["systemctl", "is-active", *SERVICES])
            raise
        return {"backup": str(keep), "observe_since": since, **observed}
    finally:
        staged.unlink(missing_ok=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--action", choices=("inspect", "update", "grants"), default="inspect")
    parser.add_argument("--config", required=True)
    parser.add_argument("--candidate")
    parser.add_argument("--require")
    parser.add_argument("--backup")
    args = parser.parse_args(argv)
    try:
        config = load_config(args.config)
        if args.action == "grants":
            result = grants(config, args.config, args.candidate or "", args.require, args.backup)
        elif args.action == "update":
            result = update(config, args.config, args.candidate or "", args.require)
        else:
            result = inspect(config)
        print(json.dumps(result, ensure_ascii=False, indent=2), flush=True)
        return 0
    except (OSError, ValueError, RuntimeError, sqlite3.Error, TypeError) as error:
        print("操作未完成：" + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
