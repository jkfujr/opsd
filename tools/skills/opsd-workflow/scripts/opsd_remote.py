"""通过本机 OpenSSH 检查、续传或更新一个明确指定的 OPSD 实例。"""

import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("action", choices=("inspect", "upload", "update"), nargs="?", default="inspect")
    result.add_argument("--host", required=True)
    result.add_argument("--identity", type=Path)
    result.add_argument("--port", type=int, default=22)
    result.add_argument("--config", default="/etc/opsd/instance.json")
    result.add_argument("--local-file", type=Path)
    result.add_argument("--remote-file")
    result.add_argument("--candidate")
    result.add_argument("--small-packets", action="store_true")
    return result


def check_remote_path(value):
    if not value or not value.startswith("/") or any(char in value for char in "\r\n\0"):
        raise ValueError("远端路径须为单行绝对路径")
    return value


def connection(options, sftp=False):
    args = ["sftp" if sftp else "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15"]
    args += ["-P" if sftp else "-p", str(options.port)]
    if options.identity:
        args += ["-i", str(options.identity.resolve())]
    if options.small_packets:
        proxy = ["node", str(Path(__file__).with_name("small_packets.cjs").resolve()), "%h", "%p"]
        command = subprocess.list2cmdline(proxy) if os.name == "nt" else shlex.join(proxy)
        args += ["-o", "ProxyCommand=" + command]
    return args


def sftp_quote(value):
    if any(char in value for char in "\r\n\0"):
        raise ValueError("传输路径不能包含换行")
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def main(argv=None):
    cli = parser()
    options = cli.parse_args(argv)
    try:
        if options.host.startswith("-") or any(char.isspace() for char in options.host) or not options.host:
            raise ValueError("SSH 目标无效")
        if not 1 <= options.port <= 65535:
            raise ValueError("SSH 端口无效")
        if options.identity and not options.identity.is_file():
            raise ValueError("SSH 私钥文件不存在")
        if options.action == "upload":
            if not options.local_file or not options.local_file.is_file():
                raise ValueError("upload 需要现有 --local-file")
            check_remote_path(options.remote_file)
            check = "import json,os,sys; p=sys.argv[1]; print(json.dumps({'exists':os.path.lexists(p),'regular':os.path.isfile(p) and not os.path.islink(p)}))"
            state = subprocess.run(connection(options) + [options.host, shlex.join(["python3", "-c", check, options.remote_file])], check=True, capture_output=True, text=True, encoding="utf-8")
            remote = json.loads(state.stdout)
            if remote["exists"] and not remote["regular"]:
                raise ValueError("远端候选路径不是普通文件")
            verb = "reput" if remote["exists"] else "put"
            batch = verb + " " + sftp_quote(options.local_file.resolve().as_posix()) + " " + sftp_quote(options.remote_file) + "\n"
            with tempfile.TemporaryDirectory(prefix="opsd-sftp-") as folder:
                path = Path(folder) / "batch"
                path.write_bytes(batch.encode("utf-8"))
                subprocess.run(connection(options, True) + ["-b", str(path), options.host], check=True)
        else:
            remote = ["python3", "-", "--action", options.action, "--config", check_remote_path(options.config)]
            if options.action == "update":
                remote += ["--candidate", check_remote_path(options.candidate)]
            source = Path(__file__).with_name("remote_instance.py").read_bytes().replace(b"\r\n", b"\n")
            subprocess.run(connection(options) + [options.host, shlex.join(remote)], input=source, check=True)
        return 0
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print("操作未完成：" + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
