#!/usr/bin/env bash
set -euo pipefail

repo_dir="${CC_CONNECT_REPO_DIR:-/Users/wuzhen/CodeProjects/cc-connect}"
install_dir="${CC_CONNECT_INSTALL_DIR:-/Users/wuzhen/.local/bin}"
branch="${CC_CONNECT_BRANCH:-agent/codex-ps-steer}"
binary_path="${install_dir}/cc-connect"

cd "${repo_dir}"

current_branch="$(git branch --show-current)"
if [[ "${current_branch}" != "${branch}" ]]; then
  printf '错误：当前分支是 %s，需要 %s\n' "${current_branch}" "${branch}" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  printf '错误：工作区不干净，请先提交或处理本地修改。\n' >&2
  exit 1
fi

git fetch upstream
git merge --no-edit upstream/main

go test ./agent/codex ./core -run 'Steer|CmdPs' -count=1
go test ./core -run TestCUJ -count=1
go test ./...
go vet ./...

version="$(git describe --tags --always)-klaaay"
make build VERSION="${version}"

if [[ -n "$(git status --porcelain)" ]]; then
  printf '错误：测试或构建修改了源码树，拒绝安装未提交产物。\n' >&2
  exit 1
fi

mkdir -p "${install_dir}"
staged_binary="$(mktemp "${install_dir}/.cc-connect.XXXXXX")"
cleanup() {
  rm -f "${staged_binary}"
}
trap cleanup EXIT
install -m 0755 ./cc-connect "${staged_binary}"
mv "${staged_binary}" "${binary_path}"
trap - EXIT

"${binary_path}" daemon restart --force
"${binary_path}" daemon status
git push origin "${branch}"
