package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/chenhg5/cc-connect/agent/antigravityhook"
	"github.com/chenhg5/cc-connect/core"
	"strings"
	"testing"
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
	b.invocationHook("Stop")
	if b.steer("too late") == nil {
		t.Fatal("accepted steering after stop")
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
