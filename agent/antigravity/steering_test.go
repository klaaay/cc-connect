package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/agent/antigravityhook"
	"github.com/chenhg5/cc-connect/core"
)

func TestSteering_InjectsInSameTurnAndRejectsLateGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, err := newAgyPermissionBridge(context.Background(), make(chan core.Event, 10), false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.steer("too early"); err == nil {
		t.Fatal("accepted idle steering")
	}
	b.beginTurn()
	if err = b.steer("use carbon"); err != nil {
		t.Fatal(err)
	}
	out := b.invocationHook("PreInvocation")
	if len(out.InjectSteps) != 1 || out.InjectSteps[0].UserMessage != "use carbon" {
		t.Fatalf("response=%+v", out)
	}
	if len(b.invocationHook("PreInvocation").InjectSteps) != 0 {
		t.Fatal("duplicate injection")
	}
	if err = b.steer("add tests"); err != nil {
		t.Fatal(err)
	}
	out = b.invocationHook("PostInvocation")
	if out.TerminationBehavior != "force_continue" || len(out.InjectSteps) != 1 {
		t.Fatalf("response=%+v", out)
	}
	if err = b.steer("one last thing"); err != nil {
		t.Fatal(err)
	}
	if b.invocationHook("Stop").Decision != "continue" {
		t.Fatal("lost pending guidance at stop")
	}
	if len(b.invocationHook("PreInvocation").InjectSteps) != 1 {
		t.Fatal("missing final guidance")
	}
	b.endTurn()
	if b.steer("too late") == nil {
		t.Fatal("accepted steering after turn end")
	}
}

func TestSteering_RelayPreservesUserRoleAndIgnoresSubagents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, err := newAgyPermissionBridge(context.Background(), make(chan core.Event, 10), false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.beginTurn()
	b.conversationID = "main"
	if err = b.steer("user instruction"); err != nil {
		t.Fatal(err)
	}
	relay := func(id string) antigravityhook.BridgeResponse {
		t.Helper()
		var output bytes.Buffer
		err := antigravityhook.Relay(strings.NewReader(`{"conversationId":"`+id+`"}`), &output, b.address, b.token, "PreInvocation")
		if err != nil {
			t.Fatal(err)
		}
		var result antigravityhook.BridgeResponse
		if err = json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if len(relay("subagent").InjectSteps) != 0 {
		t.Fatal("guidance leaked to subagent")
	}
	main := relay("main")
	if len(main.InjectSteps) != 1 || main.InjectSteps[0].UserMessage != "user instruction" {
		t.Fatalf("response=%+v", main)
	}
	b.endTurn()
	if b.steer("late") == nil {
		t.Fatal("accepted after process exit")
	}
}

func TestSteering_AfterIntermediateStopDeliversOnceUntilResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	as, err := newAntigravitySession(context.Background(), "agy", nil, t.TempDir(), "", "default", "main", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	b := as.steeringBridge
	b.beginTurn()
	hook := func(event string) antigravityhook.BridgeResponse {
		return b.invocationHookFor(event, json.RawMessage(`{"conversationId":"main"}`))
	}
	b.invocationHookFor("PreInvocation", json.RawMessage(`{"conversationId":"child"}`))
	hook("Stop")
	if err := as.SteerTurn("continue with tests"); err != nil {
		t.Fatal(err)
	}
	if hook("Stop").Decision != "continue" {
		t.Fatal("pending guidance lost at stop")
	}
	steps := hook("PreInvocation").InjectSteps
	if len(steps) != 1 || steps[0].UserMessage != "continue with tests" {
		t.Fatalf("steps=%v", steps)
	}
	if len(hook("PostInvocation").InjectSteps) != 0 {
		t.Fatal("duplicate guidance")
	}

	hook("Stop")
	if err := as.SteerTurn("arrived before final result"); err != nil {
		t.Fatal(err)
	}
	as.readStream(strings.NewReader(`{"event":"result","result":{"conversation_id":"main","status":"SUCCESS"}}`))
	if err := as.SteerTurn("after result"); err == nil {
		t.Fatal("accepted guidance after result")
	}
	b.endTurn() // 收到 result 后进程退出，不能重复报告投递失败。
	select {
	case e := <-as.events:
		if e.Type != core.EventError || !strings.Contains(e.Error.Error(), "before queued guidance could be delivered") {
			t.Fatalf("event=%+v", e)
		}
	default:
		t.Fatal("missing undelivered guidance error")
	}
	if len(as.events) != 0 {
		t.Fatal("duplicate undelivered guidance error")
	}
	b.beginTurn()
	if len(hook("PreInvocation").InjectSteps) != 0 {
		t.Fatal("undelivered guidance leaked into next turn")
	}
}
