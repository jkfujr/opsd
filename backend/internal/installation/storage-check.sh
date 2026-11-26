#!/bin/sh
set -eu
export LC_ALL=C

mode=$1
uuid=${2-}
root=/opsd
expected_mount=/
actual_mount=unknown
if [ -n "$uuid" ]; then
    expected_mount=$root:UUID=$uuid
fi

fail() {
    printf 'root=%s expected_mount=%s actual_mount=%s: %s\n' "$root" "$expected_mount" "$actual_mount" "$*" >&2
    exit 1
}

find_mount() {
    findmnt --kernel --first-only --noheadings --raw --output TARGET,SOURCE,FSTYPE,FSROOT --mountpoint "$1"
}

if mount=$(find_mount "$root"); then
    read -r target source type fsroot <<EOF
$mount
EOF
    actual_mount=$target:$source:$type
    if [ "$mode" = discover ]; then
        expected_mount=$root:UUID
    elif [ -z "$uuid" ]; then
        fail '发现未登记的独立挂载'
    fi
    if [ "$target" != "$root" ] || [ "$fsroot" != / ] || [ ! -b "$source" ]; then
        fail '需要独立块设备的文件系统根'
    fi
    disk=$(stat -Lc '%Hd:%Ld' -- "$root") || fail '无法读取独立挂载的设备号'
    source_device=$(stat -Lc '%Hr:%Lr' -- "$source") || fail '无法读取挂载源的设备号'
    if [ "$disk" != "$source_device" ]; then
        fail '挂载表设备号与实际挂载不符'
    fi
    system_disk=$(stat -Lc '%Hd:%Ld' /) || fail '无法读取系统盘的设备号'
    if [ "$disk" = "$system_disk" ]; then
        fail '独立挂载指向系统盘'
    fi
    # 跳过页缓存，捕获挂载表和 udev 缓存仍正常时的设备读错误。
    if ! probe=$(dd if="$source" of=/dev/null bs=4096 count=1 iflag=direct status=none 2>&1); then
        fail "$probe"
    fi
    if ! found=$(blkid -p -s UUID -o value -- "$source") || [ -z "$found" ]; then
        fail '无法读取文件系统 UUID'
    fi
    actual_mount=$actual_mount:UUID=$found
    if [ "$mode" = discover ]; then
        uuid=$found
    elif [ "$uuid" != "$found" ]; then
        fail '文件系统 UUID 不符'
    fi
else
    if ! mount=$(find_mount /); then
        fail '无法读取系统盘挂载'
    fi
    read -r target source type fsroot <<EOF
$mount
EOF
    actual_mount=$target:$source:$type
    if [ -n "$uuid" ]; then
        fail '缺少 /opsd 精确挂载'
    fi
    if [ "$mode" = discover ]; then
        loaded=$(systemctl show --property=LoadState --value opsd.mount) || fail '无法读取 opsd.mount'
        if [ "$loaded" != not-found ]; then
            expected_mount=$root:opsd.mount
            fail '请先挂载已配置的 /opsd'
        fi
    fi
fi

for path in "$root" "$root/node" "$root/executor"; do
    if [ -L "$path" ]; then
        fail "目录包含符号链接：$path"
    fi
    if [ "$mode" = discover ] && [ ! -e "$path" ]; then
        continue
    fi
    if [ ! -d "$path" ]; then
        fail "目录不可读：$path"
    fi
    if [ "$path" != "$root" ] && find_mount "$path" >/dev/null; then
        fail "元数据目录包含独立挂载：$path"
    fi
done

if [ -d "$root" ]; then
    owner=$(stat -c %u -- "$root") || fail '无法读取 /opsd 属主'
    perms=$(stat -c %a -- "$root") || fail '无法读取 /opsd 权限'
    if [ "$owner" -ne 0 ] || [ "$((0$perms & 022))" -ne 0 ]; then
        fail '要求 /opsd 由 root 管理'
    fi
fi

if [ "$mode" = discover ]; then
    printf '%s\n' "$uuid"
elif [ "$mode" = check ]; then
    check_directory() {
        directory=$1
        account=$2
        configured_uid=$3
        uid=$(id -u "$account" 2>/dev/null) ||
            fail "$directory actual=用户不存在 expected=owner=$account"
        group=$(getent group "$account" 2>/dev/null) ||
            fail "$directory actual=组不存在 expected=group=$account"
        gid=$(printf '%s\n' "$group" | cut -d: -f3)
        if [ "$account" = opsd ] && [ "$uid" = 0 ]; then
            fail "$directory actual=owner=opsd(0) expected=opsd 的非零 UID"
        fi
        if [ -n "$configured_uid" ] && [ "$uid" != "$configured_uid" ]; then
            fail "$directory actual=owner=$account($uid) expected=user_id=$configured_uid"
        fi
        expected=$uid:$gid:0700
        actual=$(stat -c '%u:%g:0%a (%U:%G)' -- "$directory") ||
            fail "$directory actual=无法读取 expected=$expected ($account:$account)"
        if [ "${actual%% *}" != "$expected" ]; then
            fail "$directory actual=$actual expected=$expected ($account:$account)"
        fi
    }
    check_directory "$root/node" opsd "${3-}"
    check_directory "$root/executor" root 0
fi
