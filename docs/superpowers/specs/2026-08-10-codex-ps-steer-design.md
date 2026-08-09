# Codex `/ps` 同回合转向与本地源码部署设计

## 背景

cc-connect 当前把 busy 状态下的 `/ps <message>` 直接交给
`AgentSession.Send`。Codex `exec` 后端会因此并发启动第二个
`codex exec resume`，与仍在运行的进程同时写入同一个 thread，最终触发
`thread-store conflict: ... already has an active writer`。

Codex app-server 提供 `turn/steer`，能够把补充指令追加到当前活跃 turn。
本设计只让显式 `/ps` 使用该能力；普通 busy 消息继续按现有 FIFO 队列处理。

## 目标

- `/ps <message>` 在 Codex app-server 活跃 turn 中调用 `turn/steer`。
- 使用当前 thread ID 和 `expectedTurnId`，避免补充内容误入后续 turn。
- Codex exec 等不支持 steering 的后端安全拒绝，不再调用普通 `Send`。
- 保持普通 busy 消息的排队语义不变。
- 保留现有配置、历史会话、cron 和 timer 数据。
- 从个人 fork 构建并运行 daemon，后续可持续同步官方源码。

## 非目标

- 不新增 `/steer` 命令。
- 不让普通 busy 文本自动 steer。
- 不为 Codex exec 模拟或并发实现 steering。
- 不修改 Claude Code、Gemini、Cursor 等其他 agent 的行为。
- 不清理或迁移 `~/.cc-connect` 中的持久数据。

## 方案选择

采用可选能力接口，不复用 `AgentSession.Send` 的隐式状态判断：

```go
type AgentSessionSteerer interface {
    SteerTurn(prompt string) error
}
```

该方案保持 `core` 与具体 agent 解耦。支持 steering 的 session 显式实现接口；
不支持的 session 不会走到可能启动第二个 turn 的发送路径。

未采用的方案：

- 在 app-server 的 `Send` 内检测活跃 turn：实现较短，但会让所有调用方隐式改变
  `Send` 语义，无法保证只有 `/ps` 会 steer。
- 在 `core` 中判断 Codex 类型：违反项目禁止在 `core` 硬编码 agent 名称的规则。
- 给 exec 后端并发执行 `resume`：无法满足 thread 单写者约束。

## 架构与数据流

### `/ps` 命令路径

1. `cmdPs` 验证内容非空、session 存活且当前处于 busy 状态。
2. `cmdPs` 对当前 session 断言 `AgentSessionSteerer`。
3. 不支持时返回现有的发送失败提示，并记录结构化错误；不得调用 `Send`。
4. 支持时调用 `SteerTurn(text)`。
5. app-server session 在互斥锁内读取当前 thread ID 和 turn ID。
6. 发送 `turn/steer` JSON-RPC：

```json
{
  "threadId": "<thread-id>",
  "expectedTurnId": "<active-turn-id>",
  "input": [
    {"type": "text", "text": "<ps-message>"}
  ]
}
```

7. 响应的 `turnId` 必须与请求的 active turn ID 一致，随后回复 `/ps` 已发送。

### 并发约束

- 使用独立 `steerMu` 串行处理快速连续的 `/ps`，保持到达顺序。
- 在发出请求前快照 active turn ID，并通过 `expectedTurnId` 让 Codex 拒绝过期 steer。
- steering RPC 使用有限超时，避免平台消息处理协程无限等待。
- 不清空当前 turn 的缓冲文本，不修改 `currentTurn`，不启动第二个 turn。

## 错误处理

- session 已关闭：返回明确错误。
- thread ID 为空：拒绝发送。
- active turn ID 为空：提示当前没有可 steer 的 turn。
- RPC 超时或 Codex 返回错误：记录错误并向用户返回 `/ps` 发送失败。
- 响应 turn ID 不匹配：按过期或错误响应处理。
- exec 后端：因未实现能力接口而安全拒绝，不启动 `codex exec resume`。

显式 `/ps` 不回退到普通消息队列。这样可以避免用户以为当前任务已经转向，实际却在
任务结束后才收到该消息。

## 测试策略

严格按 TDD 执行：先添加会在现有代码上失败的回归测试，再写实现。

- `core` 回归测试：busy `/ps` 必须调用 `SteerTurn`，不得调用 `Send`。
- `core` 安全测试：不支持 steering 的 busy session 不调用 `Send`。
- app-server RPC 测试：方法为 `turn/steer`，thread ID、输入和
  `expectedTurnId` 正确。
- app-server 状态测试：没有 active turn 时不写入 RPC。
- app-server 响应测试：返回 turn ID 不一致时失败。
- 并发测试：连续 steer 按顺序完成，并通过 race detector。
- CUJ：现有 `/ps` 用户流程保持可见提示一致。

验证命令包括：

```bash
go test ./agent/codex ./core -run 'Steer|CmdPs' -count=1
go test -race ./agent/codex ./core -run 'Steer|CmdPs' -count=1
go test ./core -run TestCUJ -count=1
go test ./...
go vet ./...
go build ./...
```

## Fork 与分支策略

- 个人仓库：`klaaay/cc-connect`
- `origin`：个人 fork。
- `upstream`：`chenhg5/cc-connect`。
- 本地目录：`~/CodeProjects/cc-connect`。
- 长期维护分支：`agent/codex-ps-steer`。
- daemon 始终运行该分支构建出的已安装二进制，不直接运行 npm 包。

同步官方更新时，在工作区干净且测试基线可用的前提下：

1. `git fetch upstream`。
2. 将自定义分支 rebase 到 `upstream/main`。
3. 运行测试与构建。
4. 原子替换本地二进制。
5. 重启 daemon 并验证状态。
6. 将更新后的分支推送到个人 fork。

若 rebase、测试或构建失败，更新流程立即停止，不替换正在运行的二进制。

## 本机部署与数据保护

- 安装路径：`~/.local/bin/cc-connect`，该目录已在 `PATH` 中。
- daemon 的 work dir 保持 `/Users/wuzhen/.cc-connect`。
- daemon 配置继续使用 `/Users/wuzhen/.cc-connect/config.toml`。
- 只为目标 Codex 项目增加 `backend = "app_server"`；普通 busy 行为不增加配置。
- 切换前完整备份 `/Users/wuzhen/.cc-connect`。
- 切换前确认没有活跃的 `codex exec`，避免截断正在运行的 turn。
- 先构建、测试并安装自定义二进制，再用它执行 `daemon install --force`。
- 验证自定义 daemon 正常后，卸载全局 npm `cc-connect@1.4.1`。

`sessions/`、`crons/`、`timers/`、`agent-prompts/`、日志和项目状态全部留在原
data dir，不执行 `/new`，不删除会话文件。

## 回滚

切换前保留：

- 完整的 `.cc-connect` 时间戳备份。
- 原 npm 二进制路径和版本记录。
- 上一个可用的自定义二进制副本。

若新 daemon 启动失败：

1. 停止新 daemon。
2. 恢复旧配置或移除新增的 `backend = "app_server"`。
3. 重新安装 `cc-connect@1.4.1` 并用其覆盖 daemon 安装。
4. 检查 daemon、cron、timer 和 session inventory。

数据目录不参与二进制回滚，因此历史会话和定时任务不需要恢复；备份仅作为额外保护。
