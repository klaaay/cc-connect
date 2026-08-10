# Codex app-server 项目级 Skill 禁用实施计划

1. 为 SkillRegistry 添加禁用名称过滤测试和最小实现，并通过可选 Agent 接口接入 Engine。
2. 为 Codex Agent 添加 `disabled_skills` 配置解析测试，兼容 TOML 的 `[]any` 和测试常用的
   `[]string`。
3. 添加 Skill 路径解析测试：按当前项目/user Codex Skill 目录匹配、禁用所有同名路径、
   未知名称失败。
4. 修改 app-server 命令构造测试，断言生成单个合法的
   `skills.config=[{path=...,enabled=false}]` 覆盖；未配置项目参数不变。
5. 实现 exec 与 app-server 的 session 启动注入，并保留 `/ps` steer、模型、provider、
   环境变量等现有行为。
6. 迁移 `clawclaw-codex-no-superpowers-bot`：`cmd = "codex"`，在该项目 options 内声明
   `disabled_skills`；其他项目不修改。
7. 运行定向、竞态、全量测试和 vet，构建本地版本，重启 launchd daemon。
8. 验证目标项目启动与 Skill 禁用状态、其他项目隔离、持久数据数量，然后提交并推送 fork。
