#!/usr/bin/env bash
# 交互式创建短生命周期的 Seed 密码文件。密码只从控制终端读取，路径从 stdout 返回。
set -euo pipefail

umask 077

if [ "$#" -ne 0 ]; then
  printf '用法: %s\n' "$0" >&2
  exit 2
fi
if [ ! -t 0 ]; then
  printf '无法访问交互终端；请显式使用 --password-file\n' >&2
  exit 2
fi

password=""
confirmation=""
password_file=""

cleanup() {
  password=""
  confirmation=""
  if [ -n "$password_file" ]; then
    rm -f -- "$password_file"
  fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

while true; do
  printf '请输入 Seed 管理员密码（trim 后 12–1024 字节）: ' >&2
  if ! IFS= read -r -s password; then
    printf '\n读取密码失败\n' >&2
    exit 1
  fi
  printf '\n' >&2
  printf '请再次输入密码: ' >&2
  if ! IFS= read -r -s confirmation; then
    printf '\n读取确认密码失败\n' >&2
    exit 1
  fi
  printf '\n' >&2
  if [ "$password" = "$confirmation" ]; then
    break
  fi
  printf '两次输入不一致，请重新输入。\n' >&2
done

password_file="$(mktemp "${TMPDIR:-/tmp}/vastplan-seed-password.XXXXXX")"
printf '%s' "$password" >"$password_file"
chmod 600 "$password_file"
password=""
confirmation=""

printf '%s\n' "$password_file"
password_file=""
