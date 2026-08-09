# Codex `/ps` 同回合转向实现计划

> **对于 agent 型执行者：** 必需子 skill：优先使用 `superpowers-subagent-driven-development`，否则使用 `superpowers-executing-plans` 逐任务实施本计划。所有步骤使用 `- [ ]` 复选框格式追踪。

**目标：** 让 busy 状态下的显式 `/ps <message>` 通过 Codex app-server 的 `turn/steer` 追加到当前 turn，并让不支持 steering 的 session 安全拒绝。

**架构：** `core` 通过可选的 `AgentSessionSteerer` 接口表达能力，`cmdPs` 只调用该接口。Codex app-server 实现 `SteerTurn`，串行发送带 `expectedTurnId` 的 `turn/steer` RPC；Codex exec 不实现接口，因此不会启动第二个 `codex exec resume`。

**技术栈：** Go 1.24、JSON-RPC 2.0、Codex app-server、Go testing、race detector。

## Global Constraints

- 只有显式 `/ps <message>` 才 steer；普通 busy 消息继续进入 FIFO 队列。
- 不新增 `/steer` 命令。
- 不为 Codex exec 模拟 steering，也不允许 `/ps` 回退到 `AgentSession.Send`。
- `core` 不得硬编码 `codex` 或任何具体 agent 名称。
- 所有 bug fix 必须先写失败的回归测试，再写生产代码。
- 不新增用户文案；复用现有 `MsgPsSent`、`MsgPsSendFailed`、`MsgPsNoSession`。
- steering 请求必须包含当前 thread ID 和 `expectedTurnId`。
- 快速连续的 steering 请求必须串行执行。
- 不修改 `.cc-connect` 数据文件、daemon 或本机配置；这些操作属于独立部署计划。

---

## 文件结构

- 修改 `core/interfaces.go`：定义 `AgentSessionSteerer` 可选能力接口。
- 修改 `core/engine.go`：让 `cmdPs` 使用 steering 能力，不再调用 `Send`。
- 修改 `core/engine_test.go`：增加 steering stub 和 `/ps` 回归测试。
- 修改 `agent/codex/appserver_session.go`：实现 `turn/steer` RPC、超时和并发保护。
- 修改 `agent/codex/appserver_session_test.go`：验证 RPC 请求、活跃 turn 校验和响应校验。

---

### 任务 1：让 `/ps` 只依赖显式 steering 能力

**文件：**

- 修改：`core/interfaces.go:576-583`
- 修改：`core/engine.go:6255-6290`
- 测试：`core/engine_test.go:8475-8498`
- 测试：`core/engine_test.go:10073-10168`

**Interfaces：**

- Consumes: 现有 `AgentSession`、`MsgPsSent`、`MsgPsSendFailed`、session busy lock。
- Produces: `type AgentSessionSteerer interface { SteerTurn(prompt string) error }`；`cmdPs` 仅通过该接口插入当前 turn。

- [ ] **步骤 1：安装 Go 依赖并验证修改前基线**

运行：

```bash
go mod download
go test ./core -run TestCmdPs -count=1
```

预期：命令退出码为 0；现有 `/ps` 测试通过，证明基线可用。

- [ ] **步骤 2：添加 steering 测试 session**

在 `core/engine_test.go` 的 `queuingAgentSession` 辅助类型后加入：

```go
type steeringAgentSession struct {
	*queuingAgentSession
	steerMu    sync.Mutex
	steerCalls []string
	steerErr   error
}

func newSteeringSession(id string) *steeringAgentSession {
	return &steeringAgentSession{queuingAgentSession: newQueuingSession(id)}
}

func (s *steeringAgentSession) SteerTurn(prompt string) error {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	s.steerCalls = append(s.steerCalls, prompt)
	return s.steerErr
}

func (s *steeringAgentSession) getSteerCalls() []string {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	return append([]string(nil), s.steerCalls...)
}
```

- [ ] **步骤 3：把 busy `/ps` 测试改成期望 `SteerTurn`**

将 `TestCmdPs_BusySession_InjectsToAgent` 改为：

```go
func TestCmdPs_BusySession_SteersActiveTurn(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	sess := newSteeringSession("ps-busy")
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)

	key := "test:user1"
	state := &interactiveState{agentSession: sess, platform: p}
	e.interactiveMu.Lock()
	e.interactiveStates[key] = state
	e.interactiveMu.Unlock()

	session := e.sessions.GetOrCreateActive(key)
	if !session.TryLock() {
		t.Fatal("expected TryLock to succeed")
	}
	defer session.Unlock()

	msg := &Message{SessionKey: key, Content: "/ps add unit tests", ReplyCtx: "ctx"}
	e.cmdPs(p, msg, []string{"add", "unit", "tests"})

	if calls := sess.getSteerCalls(); len(calls) != 1 || calls[0] != "add unit tests" {
		t.Fatalf("SteerTurn calls = %v, want [add unit tests]", calls)
	}
	sess.sendMu.Lock()
	sendCalls := append([]string(nil), sess.sendCalls...)
	sess.sendMu.Unlock()
	if len(sendCalls) != 0 {
		t.Fatalf("Send calls = %v, want none", sendCalls)
	}

	sent := p.getSent()
	found := false
	for _, text := range sent {
		if strings.Contains(text, e.i18n.T(MsgPsSent)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected MsgPsSent reply, got %v", sent)
	}
}
```

- [ ] **步骤 4：添加不支持 steering 时不得调用 `Send` 的回归测试**

在 `core/engine_test.go` 加入：

```go
func TestCmdPs_BusySessionWithoutSteeringCapabilityFailsSafely(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	sess := newQueuingSession("ps-no-steer")
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)

	key := "test:user1"
	e.interactiveMu.Lock()
	e.interactiveStates[key] = &interactiveState{agentSession: sess, platform: p}
	e.interactiveMu.Unlock()

	session := e.sessions.GetOrCreateActive(key)
	if !session.TryLock() {
		t.Fatal("expected TryLock to succeed")
	}
	defer session.Unlock()

	msg := &Message{SessionKey: key, Content: "/ps do not race", ReplyCtx: "ctx"}
	e.cmdPs(p, msg, []string{"do", "not", "race"})

	sess.sendMu.Lock()
	sendCalls := append([]string(nil), sess.sendCalls...)
	sess.sendMu.Unlock()
	if len(sendCalls) != 0 {
		t.Fatalf("Send calls = %v, want none", sendCalls)
	}
	sent := p.getSent()
	if len(sent) == 0 || !strings.Contains(sent[len(sent)-1], e.i18n.T(MsgPsSendFailed)) {
		t.Fatalf("expected MsgPsSendFailed, got %v", sent)
	}
}
```

- [ ] **步骤 5：运行测试并验证它们因旧实现调用 `Send` 而失败**

运行：

```bash
go test ./core -run 'TestCmdPs_BusySession' -count=1
```

预期：退出码非 0；`SteerTurn calls = []` 或 `Send calls = [...]`。失败原因必须是旧实现仍调用 `Send`，不能是编译或测试夹具错误。

- [ ] **步骤 6：定义 `AgentSessionSteerer`**

在 `core/interfaces.go` 的 `AgentSessionCanceller` 后加入：

```go
// AgentSessionSteerer is an optional capability for appending guidance to the
// currently active turn without starting a second turn.
type AgentSessionSteerer interface {
	SteerTurn(prompt string) error
}
```

- [ ] **步骤 7：让 `cmdPs` 使用 capability interface**

将 `core/engine.go` 中的 `cmdPs` 替换为：

```go
func (e *Engine) cmdPs(p Platform, msg *Message, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsEmpty))
		return
	}

	iKey := e.interactiveKeyForSessionKey(msg.SessionKey)
	e.interactiveMu.Lock()
	state, ok := e.interactiveStates[iKey]
	e.interactiveMu.Unlock()
	if !ok || state == nil {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsNoSession))
		return
	}
	state.mu.Lock()
	agentSession := state.agentSession
	state.mu.Unlock()
	if agentSession == nil || !agentSession.Alive() {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsNoSession))
		return
	}

	_, sessions := e.sessionContextForKey(msg.SessionKey)
	if session := sessions.GetOrCreateActive(msg.SessionKey); !session.Busy() {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsNoSession))
		return
	}

	steerer, ok := agentSession.(AgentSessionSteerer)
	if !ok {
		slog.Warn("ps: active agent session does not support steering", "session", msg.SessionKey)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsSendFailed))
		return
	}
	if err := steerer.SteerTurn(text); err != nil {
		slog.Error("ps: steer failed", "error", err)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsSendFailed))
		return
	}
	e.reply(p, msg.ReplyCtx, e.i18n.T(MsgPsSent))
}
```

- [ ] **步骤 8：运行 core 测试验证转绿**

运行：

```bash
go test ./core -run TestCmdPs -count=1
go test -race ./core -run TestCmdPs -count=1
```

预期：两条命令退出码均为 0。

- [ ] **步骤 9：提交 core capability 与命令路由**

```bash
git add core/interfaces.go core/engine.go core/engine_test.go
git commit -m "fix(core): steer explicit ps guidance safely"
```

---

### 任务 2：为 Codex app-server 实现 `turn/steer`

**文件：**

- 修改：`agent/codex/appserver_session.go:45-52`
- 修改：`agent/codex/appserver_session.go:132-179`
- 修改：`agent/codex/appserver_session.go:444-518`
- 测试：`agent/codex/appserver_session_test.go:345-452`

**Interfaces：**

- Consumes: `core.AgentSessionSteerer`、`appServerSession.currentTurn`、`requestWithTimeout`。
- Produces: `func (s *appServerSession) SteerTurn(prompt string) error`；`turnSteerResponse`；5 秒 steer RPC 超时。

- [ ] **步骤 1：添加成功 steering 的失败测试**

在 `agent/codex/appserver_session_test.go` 中加入：

```go
func TestAppServerSession_SteerTurnUsesExpectedActiveTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin := &lockedWriteCloser{}
	s := &appServerSession{
		ctx:     ctx,
		cancel:  cancel,
		stdin:   stdin,
		pending: make(map[int64]chan rpcResponseEnvelope),
	}
	s.alive.Store(true)
	s.threadID.Store("thread-1")
	s.currentTurn = "turn-7"

	done := make(chan error, 1)
	go func() { done <- s.SteerTurn("add unit tests") }()

	line := waitForWrittenJSONLine(t, stdin)
	var request struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params struct {
			ThreadID       string `json:"threadId"`
			ExpectedTurnID string `json:"expectedTurnId"`
			Input          []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"input"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatalf("decode request %q: %v", line, err)
	}
	if request.Method != "turn/steer" {
		t.Fatalf("method = %q, want turn/steer", request.Method)
	}
	if request.Params.ThreadID != "thread-1" || request.Params.ExpectedTurnID != "turn-7" {
		t.Fatalf("params = %#v, want thread-1/turn-7", request.Params)
	}
	if len(request.Params.Input) != 1 || request.Params.Input[0].Type != "text" || request.Params.Input[0].Text != "add unit tests" {
		t.Fatalf("input = %#v, want one text item", request.Params.Input)
	}

	s.pendingMu.Lock()
	responseCh := s.pending[request.ID]
	s.pendingMu.Unlock()
	if responseCh == nil {
		t.Fatalf("no pending RPC response channel for id %d", request.ID)
	}
	responseCh <- rpcResponseEnvelope{ID: request.ID, Result: json.RawMessage(`{"turnId":"turn-7"}`)}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SteerTurn() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SteerTurn() did not finish after RPC response")
	}
}
```

- [ ] **步骤 2：添加无活跃 turn 和响应不匹配测试**

加入：

```go
func TestAppServerSession_SteerTurnRejectsMissingActiveTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin := &lockedWriteCloser{}
	s := &appServerSession{ctx: ctx, cancel: cancel, stdin: stdin}
	s.alive.Store(true)
	s.threadID.Store("thread-1")

	err := s.SteerTurn("too late")
	if err == nil || !strings.Contains(err.Error(), "no active turn") {
		t.Fatalf("SteerTurn() error = %v, want no active turn", err)
	}
	if got := stdin.String(); got != "" {
		t.Fatalf("unexpected RPC write without active turn: %q", got)
	}
}

func TestAppServerSession_SteerTurnRejectsMismatchedTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin := &lockedWriteCloser{}
	s := &appServerSession{
		ctx:     ctx,
		cancel:  cancel,
		stdin:   stdin,
		pending: make(map[int64]chan rpcResponseEnvelope),
	}
	s.alive.Store(true)
	s.threadID.Store("thread-1")
	s.currentTurn = "turn-7"

	done := make(chan error, 1)
	go func() { done <- s.SteerTurn("late guidance") }()
	line := waitForWrittenJSONLine(t, stdin)
	var request struct{ ID int64 `json:"id"` }
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	s.pendingMu.Lock()
	responseCh := s.pending[request.ID]
	s.pendingMu.Unlock()
	if responseCh == nil {
		t.Fatalf("no pending RPC response channel for id %d", request.ID)
	}
	responseCh <- rpcResponseEnvelope{ID: request.ID, Result: json.RawMessage(`{"turnId":"turn-8"}`)}

	err := <-done
	if err == nil || !strings.Contains(err.Error(), "want \"turn-7\"") {
		t.Fatalf("SteerTurn() error = %v, want turn mismatch", err)
	}
}
```

再加入连续请求串行化测试和辅助函数：

```go
func TestAppServerSession_SteerTurnSerializesRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin := &lockedWriteCloser{}
	s := &appServerSession{
		ctx:     ctx,
		cancel:  cancel,
		stdin:   stdin,
		pending: make(map[int64]chan rpcResponseEnvelope),
	}
	s.alive.Store(true)
	s.threadID.Store("thread-1")
	s.currentTurn = "turn-7"

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- s.SteerTurn("first") }()

	firstLines := waitForWrittenJSONLines(t, stdin, 1)
	var first struct{ ID int64 `json:"id"` }
	if err := json.Unmarshal([]byte(firstLines[0]), &first); err != nil {
		t.Fatalf("decode first request: %v", err)
	}

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		secondDone <- s.SteerTurn("second")
	}()
	<-secondStarted
	time.Sleep(50 * time.Millisecond)
	if lines := nonEmptyJSONLines(stdin.String()); len(lines) != 1 {
		t.Fatalf("request count before first response = %d, want 1", len(lines))
	}

	s.pendingMu.Lock()
	firstResponse := s.pending[first.ID]
	s.pendingMu.Unlock()
	if firstResponse == nil {
		t.Fatalf("no pending RPC response channel for first id %d", first.ID)
	}
	firstResponse <- rpcResponseEnvelope{ID: first.ID, Result: json.RawMessage(`{"turnId":"turn-7"}`)}
	if err := <-firstDone; err != nil {
		t.Fatalf("first SteerTurn() error = %v", err)
	}

	lines := waitForWrittenJSONLines(t, stdin, 2)
	var second struct {
		ID int64 `json:"id"`
		Params struct {
			Input []struct{ Text string `json:"text"` } `json:"input"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("decode second request: %v", err)
	}
	if len(second.Params.Input) != 1 || second.Params.Input[0].Text != "second" {
		t.Fatalf("second input = %#v, want second", second.Params.Input)
	}
	s.pendingMu.Lock()
	secondResponse := s.pending[second.ID]
	s.pendingMu.Unlock()
	if secondResponse == nil {
		t.Fatalf("no pending RPC response channel for second id %d", second.ID)
	}
	secondResponse <- rpcResponseEnvelope{ID: second.ID, Result: json.RawMessage(`{"turnId":"turn-7"}`)}
	if err := <-secondDone; err != nil {
		t.Fatalf("second SteerTurn() error = %v", err)
	}
}

func nonEmptyJSONLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func waitForWrittenJSONLines(t *testing.T, w *lockedWriteCloser, count int) []string {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d JSON lines, buffer=%q", count, w.String())
		case <-ticker.C:
			if lines := nonEmptyJSONLines(w.String()); len(lines) >= count {
				return lines
			}
		}
	}
}
```

- [ ] **步骤 3：运行测试并验证因 `SteerTurn` 尚不存在而失败**

运行：

```bash
go test ./agent/codex -run SteerTurn -count=1
```

预期：退出码非 0，编译错误明确指出 `s.SteerTurn undefined`。

- [ ] **步骤 4：增加响应类型、互斥锁和超时常量**

在 `turnStartResponse` 后加入：

```go
type turnSteerResponse struct {
	TurnID string `json:"turnId"`
}
```

在 `appServerSession` 的状态字段中加入：

```go
	steerMu sync.Mutex
```

在现有 app-server 常量中加入：

```go
	appServerSteerTimeout = 5 * time.Second
```

- [ ] **步骤 5：实现 `SteerTurn`**

在 `Send` 后、`stageImages` 前加入：

```go
// SteerTurn appends text to the active Codex turn without starting a second
// turn. expectedTurnId prevents a late follow-up from reaching a newer turn.
func (s *appServerSession) SteerTurn(prompt string) error {
	if !s.alive.Load() {
		return fmt.Errorf("session is closed")
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("codex app-server turn/steer prompt is empty")
	}

	s.steerMu.Lock()
	defer s.steerMu.Unlock()

	threadID := s.CurrentSessionID()
	if threadID == "" {
		return fmt.Errorf("codex app-server thread id is empty")
	}
	s.stateMu.Lock()
	turnID := s.currentTurn
	s.stateMu.Unlock()
	if turnID == "" {
		return fmt.Errorf("codex app-server has no active turn to steer")
	}

	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{"type": "text", "text": prompt},
		},
		"expectedTurnId": turnID,
	}
	var resp turnSteerResponse
	if err := s.requestWithTimeout("turn/steer", params, &resp, appServerSteerTimeout); err != nil {
		return fmt.Errorf("codex app-server turn/steer: %w", err)
	}
	if resp.TurnID != turnID {
		return fmt.Errorf("codex app-server turn/steer returned turn id %q, want %q", resp.TurnID, turnID)
	}
	return nil
}
```

- [ ] **步骤 6：增加编译期接口断言**

在 `agent/codex/appserver_session_test.go` 的现有接口断言旁加入：

```go
var _ core.AgentSessionSteerer = (*appServerSession)(nil)
```

- [ ] **步骤 7：运行 app-server 测试验证转绿**

运行：

```bash
go test ./agent/codex -run SteerTurn -count=1
go test -race ./agent/codex -run SteerTurn -count=1
```

预期：两条命令退出码均为 0。

- [ ] **步骤 8：提交 Codex app-server 实现**

```bash
git add agent/codex/appserver_session.go agent/codex/appserver_session_test.go
git commit -m "feat(codex): steer active app-server turns"
```

---

### 任务 3：执行完整回归与源码质量检查

**文件：**

- 验证：`core/engine.go`
- 验证：`agent/codex/appserver_session.go`
- 验证：全仓库 Go package。

**Interfaces：**

- Consumes: 任务 1 的 `AgentSessionSteerer` 和任务 2 的 `SteerTurn`。
- Produces: 可构建、可部署的 `agent/codex-ps-steer` 源码状态。

- [ ] **步骤 1：格式化修改过的 Go 文件**

运行：

```bash
gofmt -w core/interfaces.go core/engine.go core/engine_test.go agent/codex/appserver_session.go agent/codex/appserver_session_test.go
```

- [ ] **步骤 2：运行针对性回归与 race 测试**

```bash
go test ./agent/codex ./core -run 'Steer|CmdPs' -count=1
go test -race ./agent/codex ./core -run 'Steer|CmdPs' -count=1
```

预期：两条命令退出码均为 0。

- [ ] **步骤 3：运行 CUJ、全量测试、vet 和 build**

```bash
go test ./core -run TestCUJ -count=1
go test ./...
go vet ./...
go build ./...
```

预期：四条命令退出码均为 0。若全仓库存在与本次改动无关的基线失败，必须记录精确失败项并停止部署，不得跳过。

- [ ] **步骤 4：检查 diff 与敏感信息**

```bash
git diff --check
git status -sb
git diff --stat upstream/main...HEAD
rg -n 'api[_-]?key|token|secret|password' core/interfaces.go core/engine.go core/engine_test.go agent/codex/appserver_session.go agent/codex/appserver_session_test.go
```

预期：`git diff --check` 无输出；敏感词搜索只命中既有字段或测试文案，不包含凭据。

- [ ] **步骤 5：提交格式化或验证阶段产生的必要改动**

仅在 `git status -sb` 显示预期文件有未提交修改时执行：

```bash
git add core/interfaces.go core/engine.go core/engine_test.go agent/codex/appserver_session.go agent/codex/appserver_session_test.go
git commit -m "test: cover Codex ps turn steering"
```

若工作区干净，不创建空提交。
