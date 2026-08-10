# Codex app-server 项目级 Skill 禁用设计

## 背景

Codex 0.147.0 明确拒绝 `codex --profile <name> app-server`，因此把项目 `cmd` 中的
`--profile` 透传给 app-server 会导致进程在初始化前退出。cc-connect 需要提供原生的、
项目级的 Skill 禁用配置，而不是继续依赖 app-server 不支持的 profile。

## 配置与作用域

仅 Codex 项目的 `[projects.agent.options]` 支持：

```toml
disabled_skills = [
  "superpowers-using-superpowers",
  "superpowers-brainstorming",
]
```

- 只有显式配置 `disabled_skills` 的项目生效。
- 未配置或配置空数组的项目保持现有行为，继续加载全部 Skill。
- `cmd` 只描述 CLI 可执行文件及其参数，不隐式控制 Skill。
- 同名 Skill 在多个 Codex Skill 根目录中出现时，全部禁用。
- 任一配置名称无法解析时，session 启动失败并列出未知名称，避免静默失效。

## 双层禁用

cc-connect 和 Codex app-server 会分别扫描 Skill。为了实现“真正不加载”，同一份配置
同时作用于两层：

1. cc-connect 的项目级 `SkillRegistry` 过滤禁用名称，使其不出现在管理 API、菜单和
   Skill 斜杠命令解析中。
2. Codex Agent 在启动 session 前，从当前项目的 Codex Skill 根目录解析所有匹配的
   `SKILL.md` 路径，并为 exec 或 app-server 进程追加覆盖：

```text
-c 'skills.config=[{path="/abs/path/SKILL.md",enabled=false}]'
```

Codex 官方 app-server 的 `skills/list` 已验证会把这类进程级覆盖返回为 `enabled:false`。

## 组件变化

- `core/interfaces.go`：新增可选的项目级禁用 Skill provider 接口。
- `core/skill.go`：SkillRegistry 支持按规范化名称过滤。
- `core/engine.go`：Engine 初始化时读取 Agent 的禁用名称。
- `agent/codex/codex.go`：解析 `disabled_skills`，在 session 启动前解析路径，并对 exec
  和 app-server 注入覆盖。
- `agent/codex/appserver_session.go`：命令构造接收已解析的禁用路径并生成 TOML 覆盖。

## 兼容性与错误处理

- exec 与 app-server 未配置禁用项时都不增加任何 `skills.config` 参数。
- 名称按大小写不敏感、连字符与下划线等价的规则匹配，与 cc-connect Skill 命令一致。
- 路径使用 JSON/TOML 兼容的字符串转义，不经过 shell 拼接。
- 旧 `cc-connect-no-superpowers.config.toml` 在新配置验证成功后归档移除。

## 验证

1. 单元测试覆盖配置解析、项目隔离、重复名称、未知名称和命令参数。
2. Codex agent 包测试、竞态测试、`go vet` 与全量测试通过。
3. 迁移目标项目后重建并重启 daemon。
4. 验证目标项目能启动 session，进程参数包含 `skills.config`；其他项目不包含该覆盖。
5. 验证 sessions、crons 和 timers 数量不变。
