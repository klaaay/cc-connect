# Antigravity CLI 接入

cc-connect 使用本机 `agy`，不修改其源码或二进制。需要支持 `--model`、`--effort`、`--output-format stream-json`、`PreInvocation` / `PostInvocation` / `Stop` Hook 的 CLI。

## 模型和思考强度

`/model` 的默认选项来自 `agy models`，使用项目配置与活动供应商的环境变量；供应商显式配置的模型列表仍优先。CLI 的 `-low`、`-medium`、`-high`、`-xhigh`、`-max` 变体归为模型族，`/reasoning` 按当前模型列出可用强度。配置分别保存为 `options.model` 和 `options.reasoning_effort`，启动时传入 `--model`、`--effort`。例如：

```toml
[projects.agent]
type = "antigravity"
[projects.agent.options]
work_dir = "/path/to/project"
mode = "yolo"
model = "gemini-4-carbon"
reasoning_effort = "medium"
```

Hydrogen、Carbon、Argon 的实际可用性取决于安装的 CLI 和供应商。管理接口 `/api/v1/projects/<project>/models` 保留原有字段，并通过可选的 `model_efforts` 返回每个模型的强度列表。其他 Agent 无需实现此能力。

第三方 Gemini 接口仍需配置 agy 的 `modelProvider: gemini`，并将 `GEMINI_API_KEY`、`GOOGLE_GEMINI_BASE_URL` 放入项目环境或服务环境；launchd 不会自动读取交互式 `.zshrc`。供应商的 `api_key`、`base_url` 也会映射到对应变量。Base URL 使用 agy 所需的服务根地址，避免重复 `/v1beta`。维护预检调用本地 `agy --print /model`，检查 CLI 和认证配置是否可加载，不发起模型任务，也不代表远端 Key 配额或授权已验证。

## 执行中补充指令

`/ps <补充内容>` 在当前运行任务内排队，下一次模型调用前或调用后通过 Hook 注入为用户消息。收到的是排队确认，不表示立即打断正在进行的模型请求或工具执行。任务准备停止时若还有消息，会保持原执行循环直到处理；空闲、已结束或队列超限时拒绝。进程提前退出导致未送达时会报错。子 Agent 不消费主会话的补充指令。

Hooks 使用每个会话独立的临时配置覆盖目录，保留本机 MCP、Skills 和原有 Hooks；不会写入用户的全局 hooks 文件。YOLO 仍使用 `--dangerously-skip-permissions`；默认模式沿用权限确认桥接。结构化输出用于获取真实 conversation ID，后续消息使用同一会话恢复。

## 兼容边界

Codex、Claude Code 的适配器保持原实现。模型强度目录与延迟补充提示是可选接口；未实现的 Agent 仍使用既有模型接口和 `/ps` 确认文案。
