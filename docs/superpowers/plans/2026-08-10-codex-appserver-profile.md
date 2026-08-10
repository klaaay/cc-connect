# Codex app-server `--profile` 透传实现计划

> **对于 agent 型执行者：** 必需子 skill：优先使用 `superpowers-subagent-driven-development`，否则使用 `superpowers-executing-plans` 逐任务实施本计划。所有步骤使用 `- [ ]` 复选框格式追踪。

**目标：** 让 Codex app-server 后端继承项目 `cmd` 中的 binary 和 `--profile` 等前置参数，并重新构建、推送和重启本机 daemon。

**架构：** `Agent.New` 继续使用 `core.ParseCmdOpts` 解析 `cmd`。`Agent.StartSession` 把解析结果传入 `appServerSession`，由无副作用的 `appServerCommand` 方法按“前置参数、app-server 子命令、app-server 参数”构造 argv；`connect` 只负责启动该命令。

**技术栈：** Go、Codex app-server JSON-RPC、Go testing、GNU Make、Git、macOS launchd。

## Global Constraints

- stdio app-server 继承 `cmd` 中的可执行文件和全部额外参数。
- 目标配置启动为 `codex --profile cc-connect-no-superpowers app-server --listen stdio:// ...`。
- 未设置自定义 `cmd` 的项目继续执行 `codex app-server ...`。
- 保留现有 model、reasoning effort、provider、base URL、`CODEX_HOME` 和环境变量行为。
- 不新增 `app_server_profile` 配置项。
- 不创建独立 `CODEX_HOME`，不复制认证文件。
- 不修改 exec 后端的命令构造逻辑。
- 不修改全局 skills 或现有 profile 文件。
- 不改变普通忙碌消息和 `/ps` steer 的语义。
- 只在没有活跃 Codex turn 时重启 daemon。
- 提交并推送到个人 fork 的 `agent/codex-ps-steer` 分支。

---

## 文件结构

- 修改 `agent/codex/appserver_session.go`：保存 CLI 命令配置、构造 argv、启动配置的 binary。
- 修改 `agent/codex/codex.go`：把 `cliBin` 和 `cliExtraArgs` 传给 app-server session。
- 修改 `agent/codex/appserver_session_test.go`：覆盖 profile 参数顺序、自定义 binary 和默认行为。
- 构建 `cc-connect`：生成带个人版本标识的本机二进制。

### 任务 1：用失败测试定义 app-server 命令透传行为

**文件：**

- 修改：`agent/codex/appserver_session_test.go`
- 测试：`agent/codex/appserver_session_test.go`

**Interfaces：**

- Consumes: 待实现的 `func (s *appServerSession) appServerCommand() (string, []string)`。
- Produces: profile 参数顺序和默认命令行为的回归测试。

- [ ] **步骤 1：添加 profile 与默认命令测试**

在 `agent/codex/appserver_session_test.go` 添加：

```go
func TestAppServerSession_AppServerCommandPreservesCLIProfile(t *testing.T) {
	s := &appServerSession{
		cliBin:       "/opt/custom-codex",
		cliExtraArgs: []string{"--profile", "cc-connect-no-superpowers"},
		url:          "stdio://",
		model:        "gpt-5.6-sol",
	}

	bin, args := s.appServerCommand()

	if bin != "/opt/custom-codex" {
		t.Fatalf("binary = %q, want /opt/custom-codex", bin)
	}
	want := []string{
		"--profile", "cc-connect-no-superpowers",
		"app-server", "--listen", "stdio://",
		"-c", `model="gpt-5.6-sol"`,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestAppServerSession_AppServerCommandDefaultsToCodex(t *testing.T) {
	s := &appServerSession{}

	bin, args := s.appServerCommand()

	if bin != "codex" {
		t.Fatalf("binary = %q, want codex", bin)
	}
	if want := []string{"app-server"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}
```

并在 import 中加入：

```go
"reflect"
```

- [ ] **步骤 2：运行测试并确认正确失败**

运行：

```bash
rtk go test ./agent/codex -run 'TestAppServerSession_AppServerCommand' -count=1
```

预期：编译失败，提示 `appServerSession` 没有 `cliBin`、`cliExtraArgs` 或 `appServerCommand`；失败原因必须是待实现能力缺失。

### 任务 2：实现 CLI binary 与前置参数透传

**文件：**

- 修改：`agent/codex/appserver_session.go`
- 修改：`agent/codex/codex.go`
- 测试：`agent/codex/appserver_session_test.go`

**Interfaces：**

- Consumes: `cliBin string`、`cliExtraArgs []string`、现有 app-server 运行时字段。
- Produces: `func (s *appServerSession) appServerCommand() (string, []string)`，以及接受 CLI 参数的新 `newAppServerSession` 签名。

- [ ] **步骤 1：扩展 session 字段和构造函数**

在 `appServerSession` 加入：

```go
cliBin       string
cliExtraArgs []string
```

把构造函数签名改为：

```go
func newAppServerSession(ctx context.Context, cliBin string, cliExtraArgs []string, url, workDir, model, effort, mode, resumeID, baseURL, modelProvider string, extraEnv []string, codexHome string, systemPrompt string, appendPrompt string) (*appServerSession, error)
```

在结构体初始化中复制参数：

```go
cliBin:       strings.TrimSpace(cliBin),
cliExtraArgs: append([]string(nil), cliExtraArgs...),
```

- [ ] **步骤 2：提取命令构造方法并用于 connect**

在 `connect` 前添加：

```go
func (s *appServerSession) appServerCommand() (string, []string) {
	bin := strings.TrimSpace(s.cliBin)
	if bin == "" {
		bin = "codex"
	}
	args := append([]string(nil), s.cliExtraArgs...)
	args = append(args, "app-server")
	if strings.TrimSpace(s.url) != "" {
		args = append(args, "--listen", strings.TrimSpace(s.url))
	}
	if model := strings.TrimSpace(s.model); model != "" {
		args = append(args, "-c", fmt.Sprintf("model=%q", model))
	}
	if effort := strings.TrimSpace(s.effort); effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	if provider := strings.TrimSpace(s.modelProvider); provider != "" {
		args = append(args, "-c", fmt.Sprintf("model_provider=%q", provider))
	}
	if baseURL := strings.TrimSpace(s.baseURL); baseURL != "" {
		args = append(args, "-c", fmt.Sprintf("openai_base_url=%q", baseURL))
	}
	return bin, args
}
```

把 `connect` 的命令构造替换为：

```go
bin, args := s.appServerCommand()
cmd := exec.CommandContext(s.ctx, bin, args...)
```

- [ ] **步骤 3：从 Agent 传递解析后的 CLI 配置**

把 `Agent.StartSession` 的 app-server 分支改为：

```go
if backend == "app_server" {
	return newAppServerSession(ctx, cliBin, cliExtraArgs, appServerURL, workDir, model, reasoningEffort, mode, sessionID, baseURL, provName, extraEnv, codexHome, systemPrompt, appendPrompt)
}
```

- [ ] **步骤 4：格式化并运行定向测试**

运行：

```bash
rtk gofmt -w agent/codex/appserver_session.go agent/codex/appserver_session_test.go agent/codex/codex.go
rtk go test ./agent/codex -run 'TestAppServerSession_AppServerCommand' -count=1
```

预期：新增测试全部通过。

- [ ] **步骤 5：运行 Codex 与 `/ps` 回归测试**

运行：

```bash
rtk go test ./agent/codex -count=1
rtk go test -race ./agent/codex -run 'TestAppServerSession_(AppServerCommand|SteerTurn)' -count=1
rtk go test ./core -run 'CmdPs' -count=1
```

预期：全部通过，且没有 data race。

- [ ] **步骤 6：提交功能修复**

```bash
rtk git add agent/codex/appserver_session.go agent/codex/appserver_session_test.go agent/codex/codex.go
rtk git commit -m "fix(codex): forward CLI profile to app server"
```

### 任务 3：完整验证、推送、构建与 daemon 切换

**文件：**

- 构建：`/Users/wuzhen/CodeProjects/cc-connect/cc-connect`
- 安装：`/Users/wuzhen/.local/bin/cc-connect`
- 读取：`/Users/wuzhen/.cc-connect/config.toml`
- 读取：`/Users/wuzhen/.cc-connect/daemon.json`

**Interfaces：**

- Consumes: 已提交且测试通过的 `agent/codex-ps-steer`。
- Produces: 已推送的远端分支、带 profile 透传修复的本机二进制和已重启 daemon。

- [ ] **步骤 1：运行完成前验证**

```bash
rtk go test ./agent/codex ./core -run 'AppServerCommand|SteerTurn|CmdPs' -count=1
rtk go test -race ./agent/codex ./core -run 'AppServerCommand|SteerTurn|CmdPs' -count=1
rtk go test ./...
rtk go vet ./...
rtk git diff --check
rtk git status --short --branch
```

预期：新增和 `/ps` 测试通过，`go vet` 无问题，源码树干净。若全量测试只复现已确认的 macOS `TempDir RemoveAll` 清理竞态，单独记录；任何新失败都停止部署。

- [ ] **步骤 2：推送个人 fork**

```bash
rtk git push origin agent/codex-ps-steer
rtk git rev-parse HEAD
rtk git rev-parse origin/agent/codex-ps-steer
```

预期：两个 SHA 完全一致。

- [ ] **步骤 3：构建并原子安装**

```bash
rtk make build VERSION=v1.5.0-dev-klaaay
rtk ./cc-connect --version
rtk install -m 0755 ./cc-connect /Users/wuzhen/.local/bin/.cc-connect.next
rtk mv /Users/wuzhen/.local/bin/.cc-connect.next /Users/wuzhen/.local/bin/cc-connect
rtk /Users/wuzhen/.local/bin/cc-connect --version
```

预期：源码和安装版本都显示新 commit。

- [ ] **步骤 4：确认 turn 状态并重启 daemon**

先检查目标 app-server 的日志和进程，确认没有未完成 turn；不得强杀正在处理的任务。随后运行：

```bash
rtk /Users/wuzhen/.local/bin/cc-connect daemon restart --force
rtk /Users/wuzhen/.local/bin/cc-connect daemon status
rtk jq '{work_dir, binary_path}' /Users/wuzhen/.cc-connect/daemon.json
```

预期：launchd 为 Running，binary path 仍是 `/Users/wuzhen/.local/bin/cc-connect`。

- [ ] **步骤 5：验收 profile 与持久数据**

目标项目产生新 app-server session 后运行：

```bash
rtk pgrep -lf 'codex.*app-server'
rtk find /Users/wuzhen/.cc-connect/sessions -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/crons -type f | rtk wc -l
rtk find /Users/wuzhen/.cc-connect/timers -type f | rtk wc -l
```

预期：目标进程参数包含 `--profile cc-connect-no-superpowers app-server`；sessions、crons、timers 不少于切换前的 3、3、1。若目标项目尚未创建新 session，明确记录进程级验收等待用户下一条消息触发，不能把未观察到的结果报告为通过。
