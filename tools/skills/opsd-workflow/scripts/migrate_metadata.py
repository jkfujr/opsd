"""在停服窗口内迁移单台 OPSD 的节点与执行器元数据。"""

import argparse
from contextlib import closing, contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import signal
import sqlite3
import stat
import subprocess
import sys
import time

from remote_instance import MIN_ROOT_FREE, SERVICES, config_candidate, load_config

INSTANCE = Path("/etc/opsd/instance.json")
UNIT_DIR = Path("/etc/systemd/system")
SOURCE = {"data_dir": "/var/lib/opsd/node", "executor_dir": "/var/lib/opsd/executor"}
DESTINATION = {"data_dir": "/opsd/node", "executor_dir": "/opsd/executor"}
ROOT = Path("/opsd")
CHECK = "opsd-storage-check.service"
WRITERS = {SERVICES[0]: "executor_dir", SERVICES[1]: "data_dir",
           "opsd-operation@.service": "executor_dir", "opsd-recover.service": "executor_dir"}
DATABASES = {
    ("data_dir", "node.db"): True,
    ("executor_dir", "executor.db"): True,
    ("data_dir", "metrics.sqlite"): False,
    ("data_dir", "monitoring/history.db"): False,
}
DIRECTORIES = (("data_dir", "themes"), ("executor_dir", "logs"))
EXCLUDES = ("*.bak-*", "*.pre-prune-*")


def require(condition, error):
    if not condition:
        raise RuntimeError(error)


def command(*args, allowed=(0,)):
    result = subprocess.run(args, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=45)
    if result.returncode not in allowed:
        raise RuntimeError(f"{' '.join(map(str, args))} 退出 {result.returncode}：{result.stderr.strip()}")
    return result.stdout.strip()


@contextmanager
def deadline(seconds):
    def expire(_signum, _frame):
        raise TimeoutError("本阶段超过剩余中断时间")

    require(seconds > 0, "中断时间预算已用完")
    previous = signal.signal(signal.SIGALRM, expire)
    signal.setitimer(signal.ITIMER_REAL, seconds)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def record(directory, name, value, *, best_effort=False):
    path = directory / name
    try:
        path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    except OSError as error:
        if not best_effort:
            raise
        print(f"记录 {path} 失败：{error}", file=sys.stderr, flush=True)


def sha256(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def read_db(path, readonly=True):
    return sqlite3.connect(path.as_uri() + ("?mode=ro" if readonly else "?mode=rw"), uri=True, timeout=3)


def database_state(path, check=False):
    with closing(read_db(path)) as db:
        if check:
            require(db.execute("PRAGMA integrity_check").fetchall() == [("ok",)], f"SQLite 一致性失败：{path}")
        tables = [name for name, in db.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")
                  if not name.startswith("sqlite_")]
        counts = {}
        for name in tables:
            quoted = name.replace('"', '""')
            counts[name] = db.execute(f'SELECT count(*) FROM "{quoted}"').fetchone()[0]
        result = {"schema": db.execute("PRAGMA user_version").fetchone()[0], "rows": counts}
        if "node_identity" in counts:
            public, = db.execute("SELECT public_key FROM node_identity WHERE id=1").fetchone()
            result["node_id"] = hashlib.sha256(public).hexdigest()[:32]
        return result


def service_state(config):
    command("systemctl", "is-active", *SERVICES)
    current = {}
    for unit in SERVICES:
        pid = int(command("systemctl", "show", "-p", "MainPID", "--value", unit))
        require(pid > 0, f"{unit} 缺少主进程")
        current[unit] = {"pid": pid, "sha256": sha256(Path(f"/proc/{pid}/exe"))}
    expected = sha256(Path(config["binary"]))
    require(all(value["sha256"] == expected for value in current.values()), "现行进程与磁盘工件哈希不一致")
    with closing(read_db(Path(config["data_dir"]) / "node.db")) as node, \
            closing(read_db(Path(config["executor_dir"]) / "executor.db")) as executor:
        public, body = node.execute("SELECT public_key,config FROM node_identity WHERE id=1").fetchone()
        node_id = hashlib.sha256(public).hexdigest()[:32]
        members = [key for key, in node.execute("SELECT id FROM members EXCEPT SELECT id FROM revocations ORDER BY id")]
        schemas = [db.execute("PRAGMA user_version").fetchone()[0] for db in (node, executor)]
        pending = [db.execute(f"SELECT count(*) FROM {table} WHERE terminal=0").fetchone()[0]
                   for db, table in ((node, "queue"), (executor, "operations"))]
    local = command("curl", "--fail", "--silent", "--max-time", "3", "--unix-socket",
                    config["control_socket"], "http://localhost/self")
    require(json.loads(local)["id"] == node_id, "运行中节点身份与 SQLite 不符")
    require(len(members) == 5 and node_id in members and pending == [0, 0],
            f"成员或队列状态不符：members={len(members)} pending={pending}")
    settings = json.loads(body)
    return {"binary_sha256": expected, "processes": current, "schema": schemas, "pending": pending,
            "members": members, "node_id": node_id, "node_name": settings["name"],
            "config_revision": settings["revision"], "console": settings["console"],
            "paths": {key: config[key] for key in SOURCE}}


def validate_readback(before, after):
    for key in ("binary_sha256", "schema", "pending", "members", "node_id", "node_name"):
        require(after[key] == before[key], f"读回不符：{key} actual={after[key]} expected={before[key]}")


def stop_writers(config):
    command("systemctl", "stop", "opsd.service", "opsd-executor.service")
    active = command("systemctl", "list-units", "--no-legend", "--plain",
                     "--state=active,activating,reloading,deactivating",
                     *SERVICES, "opsd-operation@*.service", "opsd-recover.service")
    require(not active, f"仍有写入服务：{active}")
    for key, name, table in (("data_dir", "node.db", "queue"), ("executor_dir", "executor.db", "operations")):
        with closing(read_db(Path(config[key]) / name)) as db:
            pending = db.execute(f"SELECT count(*) FROM {table} WHERE terminal=0").fetchone()[0]
        require(pending == 0, f"停服后仍有在途操作：{table}={pending}")


def audit_file(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode), f"预期为常规文件：{path}")
    return info


def copy_permissions(path, info):
    os.chown(path, info.st_uid, info.st_gid)
    os.chmod(path, stat.S_IMODE(info.st_mode))


def tree_entries(directory):
    require(not directory.is_symlink() and directory.is_dir(), f"活动子目录不是常规目录：{directory}")
    device = directory.stat().st_dev
    entries = {}
    for path, children, names in os.walk(directory):
        base = Path(path)
        children[:] = [name for name in children if ".bak-" not in name and ".pre-prune-" not in name]
        for name in [*children, *names]:
            if ".bak-" in name or ".pre-prune-" in name:
                continue
            item = base / name
            info = item.lstat()
            require(info.st_dev == device and (stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode)),
                    f"活动子目录包含链接、异盘或特殊文件：{item}")
            entries[str(item.relative_to(directory))] = (info.st_mode, info.st_uid, info.st_gid,
                                                        info.st_size if stat.S_ISREG(info.st_mode) else 0)
    return entries


def check_space(config):
    active = {}
    for (key, name), required in DATABASES.items():
        path = Path(config[key]) / name
        if required or path.exists() or path.is_symlink():
            active[str(path)] = audit_file(path).st_size
            wal = path.with_name(path.name + "-wal")
            if wal.exists() or wal.is_symlink():
                active[str(wal)] = audit_file(wal).st_size
    for key, name in DIRECTORIES:
        path = Path(config[key]) / name
        if path.exists() or path.is_symlink():
            active[str(path)] = sum(item[3] for item in tree_entries(path).values())
    disk = os.statvfs(ROOT if ROOT.exists() else ROOT.parent)
    available = disk.f_frsize * disk.f_bavail
    peak = 2 * sum(active.values())
    require(available >= MIN_ROOT_FREE + peak,
            f"/opsd 可用 {available} B，需留 2 GiB 和 {peak} B 复制与回退临时占用")
    return {"available_bytes": available, "peak_extra_bytes": peak, "active_bytes": active,
            "filesystem": command("findmnt", "-n", "-o", "TARGET,SOURCE,FSTYPE", "-T",
                                  ROOT if ROOT.exists() else ROOT.parent)}


def copy_state(source, destination):
    verified = {}
    for (key, name), required in DATABASES.items():
        old, new = (Path(config[key]) / name for config in (source, destination))
        if not (required or old.exists() or old.is_symlink()):
            require(not new.exists(), f"两边活动 SQLite 清单不符：{old} → {new}")
            continue
        info = audit_file(old)
        require(old.parent.resolve() == old.parent, f"数据库目录有链接：{old.parent}")
        require(new.parent.resolve() == new.parent, f"目标数据库目录有链接：{new.parent}")
        if not new.parent.exists():
            new.parent.mkdir()
            copy_permissions(new.parent, old.parent.stat())
        if new.exists() or new.is_symlink():
            audit_file(new)
        with closing(read_db(old)) as from_db, closing(sqlite3.connect(new, timeout=3)) as to_db:
            from_db.backup(to_db, pages=256, sleep=0.02)
        copy_permissions(new, info)
        state = database_state(new, check=True)
        require(state == database_state(old), f"SQLite schema 或表行数不符：{old} → {new}")
        observed = new.stat()
        verified[name] = {**state, "uid": observed.st_uid, "gid": observed.st_gid,
                          "mode": f"{stat.S_IMODE(observed.st_mode):04o}", "bytes": observed.st_size}
    for key, name in DIRECTORIES:
        old, new = (Path(config[key]) / name for config in (source, destination))
        if not old.exists() and not old.is_symlink():
            require(not new.exists(), f"两边活动目录清单不符：{old} → {new}")
            continue
        contents = tree_entries(old)
        if new.exists() or new.is_symlink():
            require(set(tree_entries(new)).issubset(contents), f"活动目录有待人工核对的删除项：{old} → {new}")
        new.mkdir(exist_ok=True)
        command("rsync", "-a", "--numeric-ids", *(f"--exclude={pattern}" for pattern in EXCLUDES),
                "--", str(old) + "/", str(new) + "/")
        require(tree_entries(new) == contents, f"活动目录条目不符：{old} → {new}")
        verified[name] = {"entries": len(contents), "size": sum(entry[3] for entry in contents.values())}
    return verified


def atomic_file(path, body, mode):
    path.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    staged = config_candidate(path, body, INSTANCE.stat())
    try:
        os.chown(staged, 0, 0)
        os.chmod(staged, mode)
        os.replace(staged, path)
    finally:
        staged.unlink(missing_ok=True)


def switch_files(config, helper):
    installed = Path(config["binary"]).parent / "opsd-storage-check"
    uid = "" if config.get("user_id") is None else str(config["user_id"])
    check = ("[Unit]\nDescription=OPSD 元数据盘自检\n\n[Service]\nType=oneshot\n"
             f'ExecStart="{installed}" check "" "{uid}"\n')
    files = {installed: (helper.read_bytes().replace(b"\r\n", b"\n"), 0o755),
             UNIT_DIR / CHECK: (check.encode(), 0o644)}
    for unit, key in WRITERS.items():
        body = ("[Unit]\nRequires=opsd-storage-check.service\nAfter=opsd-storage-check.service\n"
                f"RequiresMountsFor={DESTINATION[key]}\n")
        if key == "data_dir":
            body += f"\n[Service]\nReadWritePaths=\nReadWritePaths={DESTINATION[key]}\n"
        files[UNIT_DIR / (unit + ".d") / "60-opsd-storage-root.conf"] = (body.encode(), 0o644)
    return files


def console_write(config):
    with closing(read_db(Path(config["data_dir"]) / "node.db")) as db:
        settings = json.loads(db.execute("SELECT config FROM node_identity WHERE id=1").fetchone()[0])
    enabled = str(bool(settings["console"])).lower()
    command(config["binary"], "console", f"--enabled={enabled}")
    after = service_state(config)
    require(after["config_revision"] > settings["revision"] and after["console"] == settings["console"],
            "控制台原值写入或读回没有确认")
    return after


@contextmanager
def observe_paths(record_dir):
    with (record_dir / "file-events.log").open("w+", encoding="utf-8") as events, \
            (record_dir / "file-watch.log").open("w+", encoding="utf-8") as startup:
        env = {**os.environ, "LC_ALL": "C"}
        watcher = subprocess.Popen(
            ["inotifywait", "-m", "-r", "-e", "modify,attrib,close_write,move,delete,create",
             "--format", "%w%f %e", *SOURCE.values(), *DESTINATION.values()],
            stdout=events, stderr=startup, env=env)
        try:
            for _ in range(50):
                startup.seek(0)
                if "Watches established." in startup.read():
                    break
                require(watcher.poll() is None, "inotifywait 未启动")
                time.sleep(0.05)
            else:
                raise RuntimeError("inotifywait 未建立目录观察")
            yield
        finally:
            watcher.terminate()
            watcher.wait(timeout=3)


def migrate(record_dir, helper, expected_name, lab=None):
    require(os.geteuid() == 0, "须由 root 在已批准的单台停服窗口执行")
    require(lab is None or os.environ.get("WSL_DISTRO_NAME") == "OPSD-CI", "故障注入仅可在 OPSD-CI 执行")
    os.umask(0o077)
    with os.fdopen(os.open("/run/opsd-metadata-migration.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)) as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return migrate_locked(record_dir, helper, expected_name, lab)


def migrate_locked(record_dir, helper, expected_name, lab):
    started = time.monotonic()
    missing = [tool for tool in ("curl", "inotifywait", "lsof", "rsync") if not shutil.which(tool)]
    require(not missing, f"缺少迁移预检所需程序：{', '.join(missing)}")
    config = load_config(INSTANCE)
    require(all(config[key] == value for key, value in SOURCE.items()), "迁移仅适用约定的旧元数据布局")
    require(config["binary"] == "/opt/opsd/bin/opsd" and not config.get("storage_mount_uuid"),
            "实例程序或存储根配置与本次迁移不符")
    for source in SOURCE.values():
        require(Path(source).resolve() == Path(source) and Path(source).is_dir(), f"旧目录无效：{source}")
    for destination in DESTINATION.values():
        require(not Path(destination).exists() and not Path(destination).is_symlink(), f"目标目录已存在：{destination}")
    require(not ROOT.is_symlink() and command("findmnt", "-n", "-M", ROOT, allowed=(0, 1)) == "" and
            command("systemctl", "show", "-p", "LoadState", "--value", "opsd.mount") == "not-found",
            "本入口使用系统盘上的 /opsd")
    require(command("systemctl", "show", "-p", "ReadWritePaths", "--value", SERVICES[1]) == SOURCE["data_dir"],
            "实际节点单元的旧写入路径与计划不符")
    account = pwd.getpwnam("opsd")
    require(config.get("user_id", account.pw_uid) in (None, account.pw_uid), "实例 UID 与 opsd 账号不符")
    baseline = service_state(config)
    require(baseline["node_name"] == expected_name, f"实例名称不符：{baseline['node_name']}")
    capacity = check_space(config)
    before = INSTANCE.read_bytes()
    metadata = audit_file(INSTANCE)
    files = switch_files(config, helper)
    for path in files:
        require(not path.exists() and not path.is_symlink() and path.parent.resolve() == path.parent,
                f"迁移附加文件已存在或路径含链接：{path}")
    record_dir.mkdir(mode=0o700, parents=False)
    (record_dir / "instance.before.json").write_bytes(before)
    for unit in WRITERS:
        (record_dir / unit).write_text(command("systemctl", "cat", unit) + "\n", encoding="utf-8")
    record(record_dir, "preflight.json", {**baseline, **capacity, "added_files": [str(path) for path in files]})
    timings = {"preflight": round(time.monotonic() - started, 3)}
    next_config = {**config, **DESTINATION}
    interrupted = time.monotonic()
    last = interrupted
    installed_files = []
    target_started = False
    copied = False

    def step(name):
        nonlocal last
        now = time.monotonic()
        timings[name] = round(now - last, 3)
        last = now
        record(record_dir, "timing.json", timings, best_effort=True)

    try:
        with deadline(60):
            stop_writers(config)
            step("stop_and_drain")
            ROOT.mkdir(mode=0o755, exist_ok=True)
            require(ROOT.stat().st_uid == 0 and ROOT.stat().st_mode & 0o022 == 0, "/opsd 须由 root 管理")
            os.chmod(ROOT, 0o755)
            for directory in (ROOT / "datasets", ROOT / "datasets/data"):
                if not directory.exists() and not directory.is_symlink():
                    directory.mkdir()
                    os.chmod(directory, 0o755)
                require(directory.is_dir() and not directory.is_symlink(), f"默认数据根无效：{directory}")
            for key, dir_name in DESTINATION.items():
                path = Path(dir_name)
                path.mkdir(mode=0o700)
                owner = (account.pw_uid, account.pw_gid) if key == "data_dir" else (0, 0)
                os.chown(path, *owner)
            copied_state = copy_state(config, next_config)
            require(copied_state["node.db"]["node_id"] == baseline["node_id"], "目标节点库身份不符")
            record(record_dir, "copy.json", copied_state)
            copied = True
            step("copy_and_verify")
            if lab:
                lab("copied", next_config)
            with observe_paths(record_dir):
                for path, (body, mode) in files.items():
                    atomic_file(path, body, mode)
                    installed_files.append(path)
                require(INSTANCE.read_bytes() == before, "停服期间实例配置发生变化")
                candidate = config_candidate(INSTANCE, (json.dumps(next_config, ensure_ascii=False, indent=2) + "\n").encode(),
                                             metadata)
                os.replace(candidate, INSTANCE)
                command("systemctl", "daemon-reload")
                target_started = True
                command("systemctl", "start", *SERVICES)
                step("switch_and_start")
                after = console_write(next_config)
                validate_readback(baseline, after)
                record(record_dir, "readback.json", after)
                pids = ",".join(str(process["pid"]) for process in after["processes"].values())
                opened = command("lsof", "-nP", "-p", pids)
                for key, file in (("data_dir", "node.db"), ("executor_dir", "executor.db")):
                    require(DESTINATION[key] + "/" + file in opened, f"进程没有打开目标库：{file}")
                require(not any(path + "/" in opened for path in SOURCE.values()), "数据进程仍打开旧路径")
                (record_dir / "open-files.log").write_text(opened + "\n", encoding="utf-8")
                if lab:
                    lab("written", next_config)
            events = (record_dir / "file-events.log").read_text(encoding="utf-8").splitlines()
            old_events = sum(line.startswith(tuple(SOURCE.values())) for line in events)
            new_events = sum(line.startswith(DESTINATION["data_dir"] + "/node.db") for line in events)
            require(old_events == 0, "旧元数据目录有写入事件")
            require(new_events > 0, "缺少新节点库写入事件")
            record(record_dir, "write-counts.json", {"old_path_events": old_events, "new_node_db_events": new_events})
            step("readback_and_writes")
            result = {"status": "migrated", "elapsed": round(time.monotonic() - interrupted, 3), "timing": timings}
    except Exception as error:
        write_state = "postwrite" if target_started else "prewrite"
        step("failure_detection")
        record(record_dir, "rollback-cause.json", {"stage": write_state, "cause": str(error)}, best_effort=True)
        last = time.monotonic()
        try:
            with deadline(90 - (time.monotonic() - interrupted)):
                if target_started:
                    stop_writers(next_config)
                    if lab:
                        lab("stopped_for_rollback", next_config)
                    restored = copy_state(next_config, config)
                    record(record_dir, "rollback-copy.json", restored, best_effort=True)
                    step("restore_latest")
                if installed_files or target_started:
                    os.replace(config_candidate(INSTANCE, before, metadata), INSTANCE)
                    for path in reversed(installed_files):
                        path.unlink()
                    command("systemctl", "daemon-reload")
                command("systemctl", "reset-failed", *SERVICES)
                command("systemctl", "start", *SERVICES)
                restored = service_state(config)
                validate_readback(baseline, restored)
                record(record_dir, "rollback-readback.json", restored, best_effort=True)
                step("rollback_start_and_readback")
                result = {"status": "rolled_back", "stage": write_state, "copied": copied,
                          "elapsed": round(time.monotonic() - interrupted, 3), "timing": timings}
        except Exception as failure:
            record(record_dir, "needs-recovery.json", {"migration": str(error), "rollback": str(failure)},
                   best_effort=True)
            raise RuntimeError(f"迁移与回退均未完成，保留两份目录和材料：{record_dir}") from failure
    record(record_dir, "result.json", result)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--record", type=Path, required=True)
    parser.add_argument("--helper", type=Path, required=True)
    parser.add_argument("--expected-node", required=True)
    args = parser.parse_args()
    try:
        result = migrate(args.record, args.helper, args.expected_node)
        print(json.dumps(result, ensure_ascii=False))
        return 0 if result["status"] == "migrated" else 1
    except (OSError, RuntimeError, ValueError, sqlite3.Error, subprocess.SubprocessError) as error:
        print(f"迁移未完成：{error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
