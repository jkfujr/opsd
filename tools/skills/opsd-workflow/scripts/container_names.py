"""在 OPSD 停服且队列空闲时迁移自建容器名称；CouchDB 使用独立的最后一步。"""

import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import re
import secrets
import sqlite3
import subprocess
import tempfile


def run(args):
    return subprocess.check_output(args, text=True).strip()


def save(path, value):
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False, encoding="utf-8") as file:
        os.chmod(file.name, 0o600)
        json.dump(value, file, ensure_ascii=False)
        file.flush()
        os.fsync(file.fileno())
    os.replace(file.name, path)
    if os.name == "posix":
        descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)


def read(path):
    if path.is_symlink() or not path.is_file():
        raise RuntimeError("本机记录不是普通文件：" + str(path))
    return json.loads(path.read_text(encoding="utf-8"))


def plans(config, containers, couchdb, rollback):
    directory = Path(config["executor_dir"])
    path = directory / ("couchdb.json" if couchdb else "container-names.json")
    saved = read(path) if path.exists() else ({} if couchdb else [])
    records = []
    if rollback:
        records = [saved] if couchdb and saved.get("old") else ([] if couchdb else saved)
    else:
        for container in containers:
            labels = container["Config"].get("Labels") or {}
            full_id = container["Id"]
            if couchdb:
                if labels.get("opsd.storage") != str(directory):
                    continue
                if not saved:
                    raise RuntimeError("CouchDB 缺少本机身份记录")
                if records:
                    raise RuntimeError("当前节点存在多个 CouchDB 容器")
                if saved.get("full_id") not in (None, "", full_id):
                    raise RuntimeError("CouchDB 完整 ID 与保存记录不同")
                mounts = {m["Destination"]: m["Source"] for m in container["Mounts"]}
                if mounts.get("/opt/couchdb/data") != str(directory / "couchdb/data") or mounts.get("/opt/couchdb/etc/local.d") != str(directory / "couchdb/config"):
                    raise RuntimeError("CouchDB 挂载与当前节点不同")
                if not saved.get("storage_id"):
                    saved["storage_id"] = secrets.token_hex(16)
                records.append({"full_id": full_id, "old": "opsd-couchdb", "new": "opsd-data-couchdb-" + saved["storage_id"]})
            elif labels.get("com.opsd.database-deployment"):
                deployment_id = labels["com.opsd.database-deployment"]
                if not re.fullmatch(r"[a-zA-Z0-9_-]{1,100}", deployment_id):
                    raise RuntimeError("部署 ID 无效")
                mounts = {m["Destination"]: m["Source"] for m in container["Mounts"]}
                source = mounts.get("/etc/mysql/conf.d/opsd.cnf") or mounts.get("/etc/proxysql.cnf")
                if not source:
                    raise RuntimeError("托管数据库缺少配置挂载")
                profile = read(Path(source).parent / "profile.json")
                manifest = read(Path(source).parent / "deployment.json")
                if profile.get("id") != deployment_id or manifest.get("ID") != deployment_id or manifest.get("Fingerprint") != labels.get("com.opsd.database-spec") or profile.get("directory") != str(Path(source).parent):
                    raise RuntimeError("部署资料与容器归属不符")
                kind = profile["kind"]
                if kind not in ("galera", "proxysql"):
                    raise RuntimeError("部署程序无效")
                program = "mariadb" if kind == "galera" else kind
                records.append({"full_id": full_id, "old": "opsd-" + kind + "-" + deployment_id, "new": "opsd-database-" + program + "-" + deployment_id})
    by_id = {c["Id"]: c for c in containers}
    by_name = {c["Name"].lstrip("/"): c["Id"] for c in containers}
    for record in records:
        current = by_id.get(record["full_id"])
        if current is None or current["Name"].lstrip("/") not in (record["old"], record["new"]):
            raise RuntimeError("原容器已不存在或名称已变化")
        destination = record["old"] if rollback else record["new"]
        if by_name.get(destination, record["full_id"]) != record["full_id"]:
            raise RuntimeError("目标容器名已被占用：" + destination)
    return path, saved, records


def migrate(config, couchdb=False, rollback=False):
    for unit in ("opsd.service", "opsd-executor.service"):
        if run(["systemctl", "show", unit, "-p", "ActiveState", "--value"]) != "inactive":
            raise RuntimeError("请先停止 " + unit)
    for key, name, table in (("data_dir", "node.db", "queue"), ("executor_dir", "executor.db", "operations")):
        with closing(sqlite3.connect((Path(config[key]) / name).as_uri() + "?mode=ro", uri=True)) as db:
            if db.execute("SELECT count(*) FROM " + table + " WHERE terminal=0").fetchone()[0]:
                raise RuntimeError("存在未结束操作")
    ids = run(["docker", "ps", "-aq", "--no-trunc"]).split()
    containers = json.loads(run(["docker", "inspect", *ids])) if ids else []
    path, saved, records = plans(config, containers, couchdb, rollback)
    for record in records:
        if couchdb:
            saved.update(record)
            save(path, saved)
        elif not rollback and record not in saved:
            saved.append(record)
            save(path, saved)
        destination = record["old"] if rollback else record["new"]
        current = json.loads(run(["docker", "inspect", record["full_id"]]))[0]
        if current["Name"].lstrip("/") != destination:
            run(["docker", "rename", record["full_id"], destination])
        actual = json.loads(run(["docker", "inspect", record["full_id"]]))[0]
        if actual["Id"] != record["full_id"] or actual["Name"] != "/" + destination or any(actual["State"].get(key) != current["State"].get(key) for key in ("Running", "Status", "StartedAt")) or actual["Mounts"] != current["Mounts"]:
            raise RuntimeError("改名后容器状态或挂载改变，请按完整 ID 核对")
    return records


if __name__ == "__main__":
    cli = argparse.ArgumentParser(description=__doc__)
    cli.add_argument("--config", default="/etc/opsd/instance.json")
    cli.add_argument("--couchdb", action="store_true")
    cli.add_argument("--rollback", action="store_true")
    options = cli.parse_args()
    print(json.dumps(migrate(read(Path(options.config)), options.couchdb, options.rollback), ensure_ascii=False))
