# Codex app-server `--profile` 透传设计

## 背景

项目 `clawclaw-codex-no-superpowers-bot` 使用以下配置：

```toml
[projects.agent.options]
backend = "app_server"
app_server_url = "stdio"
cmd = "codex --profile cc-connect-no-superpowers"
```

`cc-connect` 会在 `Agent.New` 中把 `cmd` 解析为可执行文件 `codex` 和额外参数 `--profile cc-connect-no-superpowers`。`exec` 后端会使用这两部分；`app_server` 后端只传递模型、工作目录等字段，并在 `connect` 中固定执行 `codex app-server`。因此实际进程缺少 `--profile`，Codex 会加载默认配置和 Superpowers skills。

OpenAI 官方配置说明要求用 `--profile profile-name` 选择 `$CODEX_HOME/profile-name.config.toml`。本次修复必须保留该参数，不能用 skills 路径硬编码代替 profile。

## 目标

- stdio app-server 继承 `cmd` 中的可执行文件和全部额外参数。
- 目标配置启动为 `codex --profile cc-connect-no-superpowers app-server --listen stdio:// ...`。
- 未设置自定义 `cmd` 的项目继续执行 `codex app-server ...`。
- 保留现有 model、reasoning effort、provider、base URL、`CODEX_HOME` 和环境变量行为。
- 修复后重新构建、安装并重启本地 launchd daemon。
- 提交并推送到个人 fork 的 `agent/codex-ps-steer` 分支。

## 非目标

- 不新增 `app_server_profile` 配置项。
- 不创建独立 `CODEX_HOME`，不复制认证文件。
- 不修改 exec 后端的命令构造逻辑。
- 不修改全局 skills 或现有 profile 文件。
- 不改变普通忙碌消息和 `/ps` steer 的语义。

## 方案

### 参数传递

`Agent.StartSession` 已经读取 `cliBin` 和 `cliExtraArgs`。app-server 分支把它们传给 `newAppServerSession`，后者在 session 中保存副本。

`connect` 按以下顺序构造命令：

```text
<cliBin> <cliExtraArgs...> app-server [--listen URL] [-c key=value ...]
```

目标项目得到：

```text
codex --profile cc-connect-no-superpowers app-server --listen stdio:// -c model="gpt-5.6-sol"
```

额外参数必须位于 `app-server` 之前，因为它们来自用户配置的 Codex 全局命令前缀。app-server 自身的 `--listen` 和运行时 `-c` 覆盖项仍由 cc-connect 追加。

### 组件变化

1. `agent/codex/codex.go`
   - app-server 分支向构造函数传递 `cliBin`、`cliExtraArgs`。
2. `agent/codex/appserver_session.go`
   - session 增加 CLI binary 与 prefix args 字段。
   - 构造函数复制参数切片，避免调用方后续修改。
   - 提取一个无副作用的命令构造函数，返回 binary 和 args。
   - `connect` 使用构造结果，并把 prefix args 放在 `app-server` 前。
3. `agent/codex/appserver_session_test.go`
   - 增加命令构造回归测试，证明 profile 参数不会丢失。
   - 覆盖默认 binary/空 prefix args，防止默认行为回归。

## 数据流

```text
config.toml 的 cmd
  → core.ParseCmdOpts
  → Agent.cmd + Agent.cliExtraArgs
  → Agent.StartSession
  → newAppServerSession
  → appServerSession.connect
  → exec.CommandContext(cliBin, cliExtraArgs + app-server args)
```

profile 仍由 Codex 自己解析。cc-connect 只负责无损传递参数，不读取或解释 profile 文件内容。

## 错误处理与兼容性

- `Agent.New` 继续用 `exec.LookPath(cliBin)` 提前校验 binary。
- 不重新解析或拼接 shell 字符串；继续使用 `core.ParseCmdOpts` 的结果，避免引入 shell 注入路径。
- 参数切片为空时，命令与当前 app-server 行为一致。
- app-server 启动、初始化或 thread 恢复失败时，沿用现有错误返回和 session 清理逻辑。
- stdio 和 WebSocket listen URL 都使用同一命令构造路径，因此都会继承 binary 与 prefix args。

## 测试策略

按 TDD 实施：

1. 先添加失败测试，通过命令构造函数断言 profile 参数位于 `app-server` 前，确认当前实现缺少该能力。
2. 添加默认命令测试，断言无额外参数时仍是 `codex app-server ...`，并覆盖自定义 binary。
3. 编写最小实现使测试通过。
4. 运行 Codex agent 包测试和竞态测试。
5. 运行现有 `/ps` steer 测试，确保本次命令修复不影响中途插话。
6. 运行 `go vet ./...`、构建和现有全量测试；已知 macOS `TempDir RemoveAll` 清理竞态单独记录，不与新回归混淆。

## 部署与验收

1. 提交并推送 `agent/codex-ps-steer`。
2. 构建带个人标识的二进制，原子替换 `/Users/wuzhen/.local/bin/cc-connect`。
3. 确认没有活跃 Codex turn 后重启 daemon。
4. 在目标项目创建新 session，不能复用修复前已启动且缺少 profile 的 app-server 进程。
5. 用进程参数确认存在 `--profile cc-connect-no-superpowers`。
6. 发送普通消息，确认不会读取 `superpowers-using-superpowers/SKILL.md`。
7. 确认 daemon、cron、sessions 和 timers 保持正常。

## 回滚

若新版本无法启动，重新安装备份版本或从切换前提交构建二进制，并让 launchd 继续指向 `/Users/wuzhen/.local/bin/cc-connect`。配置和持久数据目录不迁移，因此回滚不需要恢复 sessions、crons 或 timers。
