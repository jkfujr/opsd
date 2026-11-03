#!/bin/sh
set -eu

version='@OPSD_VERSION@'
fail() { printf '%s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail '请以 root 执行安装命令'
[ "$(uname -s)" = Linux ] || fail '仅支持 Linux'
[ -f /etc/os-release ] || fail '无法识别系统'
. /etc/os-release
case "$ID" in debian|ubuntu) ;; *) fail '仅支持 Debian 或 Ubuntu' ;; esac
[ -d /run/systemd/system ] || fail '请在 systemd 主机运行安装命令'
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail '仅支持 amd64 或 arm64' ;; esac
case "$version" in @*) fail '请使用 Releases 中的安装脚本' ;; esac

if { ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; } || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl
fi
download() {
    if command -v curl >/dev/null 2>&1; then
        curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 15 "$1" -o "$2"
    else
        wget --https-only --timeout=30 -q -O "$2" "$1"
    fi
}
directory=$(mktemp -d)
trap 'rm -rf "$directory"' 0 1 2 15
download "https://github.com/jkfujr/opsd/releases/download/v$version/opsd-linux-$arch" "$directory/opsd"
chmod 700 "$directory/opsd"
[ "$("$directory/opsd" version --plain)" = "$version" ] || fail '下载的程序版本不匹配'
"$directory/opsd" install --expected-version "$version" "$@"
