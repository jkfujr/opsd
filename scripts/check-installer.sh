#!/bin/sh
set -eu

[ "$(id -u)" -eq 0 ] || { printf '%s\n' '请以 root 运行安装失败路径检查' >&2; exit 1; }
directory=$(mktemp -d)
trap 'rm -rf "$directory"' 0 1 2 15
mkdir "$directory/bin"
cat > "$directory/bin/curl" <<'MOCK'
#!/bin/sh
set -eu
for target do :; done
touch "$OPSD_INSTALL_CALLED"
if [ "$OPSD_INSTALL_CASE" = interrupted ]; then
  printf '%s\n' '下载中断' > "$target"
  exit 18
fi
cat > "$target" <<'BINARY'
#!/bin/sh
if [ "$1" = version ]; then printf '%s\n' 'wrong-version'; exit 0; fi
touch "$OPSD_INSTALL_SENTINEL"
BINARY
MOCK
chmod 700 "$directory/bin/curl"
for scenario in interrupted wrong-version; do
  if env PATH="$directory/bin:$PATH" OPSD_INSTALL_CASE="$scenario" OPSD_INSTALL_CALLED="$directory/$scenario.called" OPSD_INSTALL_SENTINEL="$directory/installed" sh artifacts/install.sh --non-interactive > "$directory/$scenario.log" 2>&1; then
    printf '%s\n' "失败路径被接受：$scenario" >&2
    exit 1
  fi
  test -e "$directory/$scenario.called"
  test ! -e "$directory/installed"
done
grep -q '下载的程序版本不匹配' "$directory/wrong-version.log"
if artifacts/opsd-linux-amd64 install --non-interactive --expected-version wrong-version --config "$directory/instance.json" > "$directory/version.log" 2>&1; then exit 1; fi
grep -q '安装程序版本与接入命令不匹配' "$directory/version.log"

if artifacts/opsd-linux-amd64 install --expected-version wrong-version -e 'http://127.0.0.1:8444' -k '节点公钥' -t '一次性接入码' -n "名称'含空格" -r '测试地域' > "$directory/short-options.log" 2>&1; then exit 1; fi
grep -q '安装程序版本与接入命令不匹配' "$directory/short-options.log"
test ! -e "$directory/instance.json"
printf '%s\n' '安装失败路径通过：下载中断、程序版本错误、接入版本错误及全部短参数。'
