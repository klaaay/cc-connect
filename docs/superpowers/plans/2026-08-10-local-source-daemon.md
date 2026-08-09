# cc-connect 本地源码 daemon 部署计划

> **对于 agent 型执行者：** 必需子 skill：优先使用 `superpowers-subagent-driven-development`，否则使用 `superpowers-executing-plans` 逐任务实施本计划。所有步骤使用 `- [ ]` 复选框格式追踪。

**目标：** 从 `klaaay/cc-connect` 的长期维护分支构建 cc-connect，安全替换 npm 官方安装、复用现有数据目录运行 launchd daemon，并提供可重复的上游同步脚本。

**架构：** 源码保存在 `~/CodeProjects/cc-connect`，自定义二进制安装到 `~/.local/bin/cc-connect`，launchd 始终指向该稳定路径。更新脚本先同步、测试和构建，成功后才原子替换二进制并重启 daemon；现有 `~/.cc-connect` 原地复用并在首次切换前完整备份。

**技术栈：** Bash、Git/GitHub、Go、GNU Make、npm、macOS launchd、cc-connect daemon CLI。

## Global Constraints

- 个人 fork 为 `klaaay/cc-connect`；`origin` 指向 fork，`upstream` 指向 `chenhg5/cc-connect`。
- 长期维护分支为 `agent/codex-ps-steer`。
- 本地源码目录固定为 `/Users/wuzhen/CodeProjects/cc-connect`。
- 自定义二进制固定安装到 `/Users/wuzhen/.local/bin/cc-connect`。
- daemon work dir 保持 `/Users/wuzhen/.cc-connect`。
- daemon 配置保持 `/Users/wuzhen/.cc-connect/config.toml`。
- 只修改项目 `clawclaw-codex-no-superpowers-bot` 的 Codex 后端配置。
- 目标项目必须同时设置 `backend = "app_server"` 和 `app_server_url = "stdio"`。
- 不删除或迁移 `sessions/`、`crons/`、`timers/`、`agent-prompts/`、`logs/`。
- 切换前必须确认没有活跃的 `codex exec` 或未完成 turn。
- 测试或构建失败时不得替换二进制、重启 daemon 或卸载 npm 包。
- 自定义 daemon 验证成功后才卸载全局 npm `cc-connect@1.4.1`。

---

## 文件结构

- 创建 `scripts/local-source-update.sh`：同步 upstream、验证、构建、原子安装、重启和推送。
- 修改 `/Users/wuzhen/.cc-connect/config.toml`：目标项目启用 Codex stdio app-server。
- 创建 `/Users/wuzhen/.cc-connect-backups/<timestamp>/`：首次切换前的数据快照。
- 安装 `/Users/wuzhen/.local/bin/cc-connect`：daemon 与交互式 CLI 的稳定入口。
- 更新 `/Users/wuzhen/.cc-connect/daemon.json` 和 launchd plist：由 `daemon install --force` 自动完成。

---

### 任务 1：创建可重复的源码更新脚本

**文件：**

- 创建：`scripts/local-source-update.sh`
- 验证：`scripts/local-source-update.sh`

**Interfaces：**

- Consumes: 干净的 `agent/codex-ps-steer` 分支、`origin`、`upstream`、Go、Make、launchd daemon。
- Produces: `/Users/wuzhen/.local/bin/cc-connect` 和已重启的源码版 daemon；失败时保留原二进制与 daemon。

- [ ] **步骤 1：写更新脚本的 shell 语法失败测试**

在脚本尚不存在时运行：

```bash
rtk bash -n scripts/local-source-update.sh
```

预期：退出码非 0，并提示文件不存在。

- [ ] **步骤 2：创建更新脚本**

创建 `scripts/local-source-update.sh`：

```bash
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
```

- [ ] **步骤 3：设置执行权限并验证 shell 语法**

运行：

```bash
rtk chmod 0755 scripts/local-source-update.sh
rtk bash -n scripts/local-source-update.sh
```

预期：两条命令退出码均为 0。

- [ ] **步骤 4：验证脚本的分支与 dirty-worktree 防护**

只做静态验证，不运行会 fetch/rebase/restart 的完整脚本：

```bash
rtk rg -n 'branch=.*agent/codex-ps-steer|git status --porcelain|git fetch upstream|git merge --no-edit upstream/main|go test ./\.\.\.|make build|daemon restart --force|git push origin' scripts/local-source-update.sh
```

预期：每个保护或更新阶段都有命中。

- [ ] **步骤 5：提交更新脚本**

```bash
rtk git add scripts/local-source-update.sh
rtk git commit -m "chore: add local source deployment workflow"
```

---

### 任务 2：备份数据并准备目标 Codex 配置

**文件：**

- 读取：`/Users/wuzhen/.cc-connect/config.toml`
- 修改：`/Users/wuzhen/.cc-connect/config.toml`
- 创建：`/Users/wuzhen/.cc-connect-backups/<timestamp>/`

**Interfaces：**

- Consumes: 当前 `.cc-connect` 数据目录和目标项目配置。
- Produces: 完整数据备份；目标项目使用 stdio app-server；其他项目配置不变。

- [ ] **步骤 1：记录切换前 inventory**

运行：

```bash
rtk jq '{work_dir,log_file,binary_path,installed_at}' /Users/wuzhen/.cc-connect/daemon.json
rtk find /Users/wuzhen/.cc-connect/sessions -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/crons -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/timers -type f | rtk wc -l
rtk du -sh /Users/wuzhen/.cc-connect
```

预期：daemon work dir 为 `/Users/wuzhen/.cc-connect`，binary path 仍为 npm 安装路径；记录三个文件数和目录大小供切换后比对。

- [ ] **步骤 2：确认没有活跃 Codex turn**

运行：

```bash
rtk pgrep -lf 'codex exec|codex app-server'
```

预期：没有属于 cc-connect 活跃 turn 的进程。如果存在，停止部署并等待 turn 完成；不得强制杀死正在工作的 Codex。

- [ ] **步骤 3：创建固定备份目录并复制全部数据**

先确认目标不存在，再复制到精确路径：

```bash
rtk test ! -e /Users/wuzhen/.cc-connect-backups/20260810-before-source-daemon
rtk mkdir -p /Users/wuzhen/.cc-connect-backups/20260810-before-source-daemon
rtk cp -a /Users/wuzhen/.cc-connect/. /Users/wuzhen/.cc-connect-backups/20260810-before-source-daemon/
rtk du -sh /Users/wuzhen/.cc-connect-backups/20260810-before-source-daemon
```

预期：备份大小与源目录同量级，且包含 `config.toml`、`sessions`、`crons`、`timers`。

- [ ] **步骤 4：在目标项目中显式启用 stdio app-server**

在 `/Users/wuzhen/.cc-connect/config.toml` 中找到：

```toml
[[projects]]
name = "clawclaw-codex-no-superpowers-bot"
```

在该项目的 `[projects.agent.options]` 下加入：

```toml
backend = "app_server"
app_server_url = "stdio"
```

不得修改其他项目的 agent options。

- [ ] **步骤 5：验证配置只包含预期变更**

运行：

```bash
rtk cc-connect doctor --summary
rtk rg -n -C 12 'name = "clawclaw-codex-no-superpowers-bot"|backend = "app_server"|app_server_url = "stdio"' /Users/wuzhen/.cc-connect/config.toml
```

预期：doctor 不报告 TOML 解析错误；目标项目附近出现两个新增配置值。

---

### 任务 3：构建并安装自定义二进制

**文件：**

- 构建：`/Users/wuzhen/CodeProjects/cc-connect/cc-connect`
- 安装：`/Users/wuzhen/.local/bin/cc-connect`

**Interfaces：**

- Consumes: 已通过完整测试的 `agent/codex-ps-steer` 分支。
- Produces: 带个人版本标识的可执行文件，但尚未修改 daemon。

- [ ] **步骤 1：运行部署前完整验证**

```bash
rtk go test ./agent/codex ./core -run 'Steer|CmdPs' -count=1
rtk go test -race ./agent/codex ./core -run 'Steer|CmdPs' -count=1
rtk go test ./core -run TestCUJ -count=1
rtk go test ./...
rtk go vet ./...
```

预期：所有命令退出码为 0。

- [ ] **步骤 2：构建带个人标识的完整二进制**

运行：

```bash
rtk git describe --tags --always
rtk make build VERSION=v1.5.0-dev-klaaay
rtk ./cc-connect --version
```

预期：构建退出码为 0；版本输出包含 `klaaay` 和当前 commit。

- [ ] **步骤 3：原子安装到稳定路径**

运行：

```bash
rtk mkdir -p /Users/wuzhen/.local/bin
rtk install -m 0755 ./cc-connect /Users/wuzhen/.local/bin/.cc-connect.next
rtk mv /Users/wuzhen/.local/bin/.cc-connect.next /Users/wuzhen/.local/bin/cc-connect
rtk /Users/wuzhen/.local/bin/cc-connect --version
```

预期：最终命令显示自定义版本；npm 安装仍保留，daemon 尚未切换。

---

### 任务 4：切换 launchd daemon 后移除 npm 官方包

**文件：**

- 修改：`/Users/wuzhen/.cc-connect/daemon.json`
- 修改：macOS launchd 的 cc-connect LaunchAgent plist。
- 删除：全局 npm 包 `cc-connect@1.4.1`。

**Interfaces：**

- Consumes: `/Users/wuzhen/.local/bin/cc-connect`、原 data dir、更新后的配置。
- Produces: 由自定义二进制运行的 launchd daemon；shell 中的 `cc-connect` 解析到 `~/.local/bin`。

- [ ] **步骤 1：用自定义二进制覆盖 daemon 安装**

运行：

```bash
rtk /Users/wuzhen/.local/bin/cc-connect daemon install --force --config /Users/wuzhen/.cc-connect/config.toml
rtk /Users/wuzhen/.local/bin/cc-connect daemon status
```

预期：状态为 `Running`，且出现新的 PID。

- [ ] **步骤 2：验证 daemon 指向自定义二进制**

```bash
rtk jq '{work_dir,log_file,binary_path,installed_at}' /Users/wuzhen/.cc-connect/daemon.json
rtk pgrep -lf cc-connect
rtk /Users/wuzhen/.local/bin/cc-connect daemon logs -n 80
```

预期：`binary_path` 为 `/Users/wuzhen/.local/bin/cc-connect`；进程命令行使用该路径；日志没有配置解析、app-server 初始化或平台连接 fatal error。

- [ ] **步骤 3：确认新 daemon 稳定后卸载 npm 官方包**

先解析并确认精确卸载目标：

```bash
rtk npm list -g cc-connect --depth=0
```

输出必须显示全局 `cc-connect@1.4.1`。随后执行：

```bash
rtk npm uninstall -g cc-connect
rtk npm list -g cc-connect --depth=0
rtk which cc-connect
rtk cc-connect --version
```

预期：npm list 不再包含 cc-connect；`which` 返回 `/Users/wuzhen/.local/bin/cc-connect`；版本包含 `klaaay`。

- [ ] **步骤 4：验证持久数据 inventory 未丢失**

```bash
rtk find /Users/wuzhen/.cc-connect/sessions -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/crons -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/timers -type f | rtk wc -l
rtk cc-connect daemon status
rtk cc-connect doctor --summary
```

预期：三个文件数不小于切换前记录；daemon 为 `Running`；doctor 不报告配置或 Codex CLI 错误。

- [ ] **步骤 5：推送长期维护分支**

```bash
rtk git status -sb
rtk git push -u origin agent/codex-ps-steer
rtk gh api repos/klaaay/cc-connect/branches/agent/codex-ps-steer --jq '{name,sha:.commit.sha}'
```

预期：工作区干净；GitHub fork 上的分支 SHA 与本地 `HEAD` 一致。

---

### 任务 5：执行首次源码同步演练与回滚检查

**文件：**

- 执行：`scripts/local-source-update.sh`
- 读取：`.git/remotes`、daemon metadata、日志和备份目录。

**Interfaces：**

- Consumes: 已提交并推送的维护分支、已运行的自定义 daemon。
- Produces: 可重复验证的更新流程和明确回滚入口。

- [ ] **步骤 1：验证 remote 和工作区前置条件**

```bash
rtk git remote -v
rtk git status -sb
rtk git branch --show-current
```

预期：`origin` 为 `klaaay/cc-connect`，`upstream` 为 `chenhg5/cc-connect`，工作区干净，分支为 `agent/codex-ps-steer`。

- [ ] **步骤 2：执行更新脚本**

```bash
rtk ./scripts/local-source-update.sh
```

预期：fetch、merge、测试、构建、daemon restart 和 push 全部成功；若 upstream 没有新提交，merge 报告已是最新状态。

- [ ] **步骤 3：执行最终服务和数据验证**

```bash
rtk cc-connect --version
rtk cc-connect daemon status
rtk jq '.binary_path' /Users/wuzhen/.cc-connect/daemon.json
rtk find /Users/wuzhen/.cc-connect-backups -maxdepth 2 -name config.toml
rtk cc-connect daemon logs -n 120
```

预期：版本包含 `klaaay`；daemon 为 `Running`；metadata 指向自定义二进制；备份存在；日志没有新的 fatal error。

- [ ] **步骤 4：记录回滚命令，但不主动执行**

仅当自定义 daemon 后续失败时执行：

```bash
rtk /Users/wuzhen/.local/bin/cc-connect daemon stop
rtk npm install -g cc-connect@1.4.1
rtk cc-connect daemon install --force --config /Users/wuzhen/.cc-connect/config.toml
rtk cc-connect daemon status
```

若失败来自 app-server 连接，再从时间戳备份恢复原 `config.toml`。不得覆盖 `sessions/`、`crons/` 或 `timers/`。
