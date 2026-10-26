# Webhook Notifier 命令集成

在需要控制 Notifier 的项目中配置一次连接。命令直接调用 Notifier 管理接口，不调用 Agent，不创建 Cron，也不改变任务原来的执行槽位。

```toml
[[projects]]
name = "work-manager"
admin_from = "你的平台用户 ID"

[projects.webhook_notifier]
base_url = "http://127.0.0.1:6682/api/admin/v1"
token_file = "/absolute/path/to/notifier/admin-token"
```

此示例仅展示新增字段；保留该项目原有 Agent、平台配置。`token_file` 使用 Notifier 现有管理令牌文件，建议权限 `0600`，不要把令牌写入 TOML。远端连接要求 HTTPS，本机回环地址可用 HTTP。此集成使用现有管理身份，具有该 Notifier 的管理范围，不提供另一个按任务授权系统。`/wn_tasks`、`/wn_deploy`、`/wn_runs` 及后续选择均检查项目 `admin_from` 和 disabled commands；只给预期操作者开放。

- `/wn_tasks`：读取实时任务目录，按 Notifier 的置顶顺序展示；`📌` 在序号前。每页 10 项，序号在本次列表中连续。
- `/wn_deploy`：按仓库、环境和应用展开部署选项，选择后输入本次发布分支；取消或非法分支不会提交。服务端在提交时核验远端分支并固定候选 SHA。
- `/wn_runs`：查看运行中和最近执行，回复序号读取最新状态。
- `/wn_help`：查看交互帮助。

收到列表后回复序号；回复 `下一页` / `上一页` 或 `next` / `prev` 翻页，`取消` / `cancel` 退出。参数根据任务模板逐项收集，选项用序号选择；可选字段可回复 `跳过` / `skip`。重新发送命令退出之前的选择。

选择按项目、平台、会话和用户隔离，闲置 5 分钟失效。列表快照保存稳定任务 ID 和配置 revision，置顶变化不会让序号指向其他任务；任务修改或停用会在提交时被拒绝。受理后重复数字不会再次执行。回复序号不占 Agent 执行槽位。

每次只提交一次写请求。网络异常时展示 requestId，先查询 Notifier 原记录，不自动重放。受理不代表成功；任务、部署的成功、失败和中断均读取服务端记录。

已受理执行的结果回传目标保存在 `data_dir/webhook-notifier/`。服务重启后，支持重建回复上下文的平台会恢复查询并回传原会话；选择中的菜单不会恢复。自动跟踪最长 24 小时，超过后提示通过 `/wn_runs` 查询，不判为执行失败。通知发送失败会继续尝试，进程在发送成功与回执落盘之间退出时可能重复通知，但不会重复执行任务。提交响应丢失或回执保存失败时仍需按请求标识核验。

服务端需要提供 `/admin/v1/wn/catalog`、`/admin/v1/wn/runs`、`/admin/v1/wn/runs/:kind/:id`，以及现有任务执行和部署接口。目录 `version` 必须为 `2`；不兼容时拒绝继续，不猜测接口。

部署请求只包含 `projectPath`、`branch`、`environment`、`target`、`requestId`，不再调用旧候选接口或提交 `taskId`、`candidateSha`。

## 分支参数选择

Notifier 的系统配置按 UED 分支、开发主分支、开发分支、发布分支统一维护三个分支任务的候选。客户端读取 `inputs.options`、`defaultValue` 与 `excludeField`，不在 cc-connect 内另存分支名单或默认值。

- 单选回复编号；多选（`type: lines` 且有 `options`）回复 `1,3`、`1，3` 或 `1 3`。整次回复替换目标选择，重复或越界编号被拒绝。
- 回复 `默认` / `default` 使用当前展示的默认值。目标菜单排除已选来源，默认值不在当前候选内时必须重新选择。
- 参数收集后展示摘要，回复 `执行` / `execute` 才提交；回复 `取消` / `cancel` 退出。重复确认不会再次提交。
- UI 合入为 UED → 开发主分支；同步为开发主分支 → 多个开发分支；开发合入发布为开发主分支 → 发布分支（仅创建 MR）。服务端再次校验分类范围，并冻结本次输入。

升级顺序可先更新 cc-connect，再在 MR 合入后更新 Notifier 与管理台。没有执行输入的旧服务端任务保留原交互，新参数在服务端更新后自动出现在 `/wn_tasks`。
