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
- `/wn_deploy`：将目录中的发布工作区、环境和应用展开为独立选项；当前单工作区直接显示 QA Web、QA Web Hub、PROD Web、PROD Web Hub 四项，回复序号即提交对应部署。多个工作区附带项目、分支和任务标识以区分。候选 SHA 由服务端读取并随提交固定，不接收任意 Shell 或分支。
- `/wn_runs`：查看运行中和最近执行，回复序号读取最新状态。
- `/wn_help`：查看交互帮助。

收到列表后回复序号；回复 `下一页` / `上一页` 或 `next` / `prev` 翻页，`取消` / `cancel` 退出。参数根据任务模板逐项收集，选项用序号选择；可选字段可回复 `跳过` / `skip`。重新发送命令退出之前的选择。

选择按项目、平台、会话和用户隔离，闲置 5 分钟失效。列表快照保存稳定任务 ID 和配置 revision，置顶变化不会让序号指向其他任务；任务修改或停用会在提交时被拒绝。受理后重复数字不会再次执行。回复序号不占 Agent 执行槽位。

每次只提交一次写请求。网络异常时展示 requestId，先查询 Notifier 原记录，不自动重放。受理不代表成功；任务、部署的成功、失败和中断均读取服务端记录。

已受理执行的结果回传目标保存在 `data_dir/webhook-notifier/`。服务重启后，支持重建回复上下文的平台会恢复查询并回传原会话；选择中的菜单不会恢复。自动跟踪最长 24 小时，超过后提示通过 `/wn_runs` 查询，不判为执行失败。通知发送失败会继续尝试，进程在发送成功与回执落盘之间退出时可能重复通知，但不会重复执行任务。提交响应丢失或回执保存失败时仍需按请求标识核验。

服务端需要提供 `/admin/v1/wn/catalog`、`/admin/v1/wn/runs`、`/admin/v1/wn/runs/:kind/:id`，以及现有任务执行、发布候选和部署接口。目录 `version` 必须为 `1`；不兼容时拒绝继续，不猜测接口。
