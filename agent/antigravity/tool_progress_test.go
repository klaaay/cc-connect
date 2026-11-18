package antigravity

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// 录自本机 agy stream-json；仅替换会话 ID 和临时工作目录。
func TestStream_ToolProgressFromRealCLI(t *testing.T) {
	data, err := os.ReadFile("testdata/tool-stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	s := &antigravitySession{ctx: context.Background(), events: make(chan core.Event, 32)}
	s.readStream(strings.NewReader(string(data)))
	var tools []core.Event
	var text string
	for len(s.events) > 0 {
		e := <-s.events
		switch e.Type {
		case core.EventToolUse, core.EventToolResult:
			tools = append(tools, e)
		case core.EventText:
			text += e.Content
		}
	}
	if len(tools) != 4 {
		t.Fatalf("expected two tool starts and results, got %+v", tools)
	}
	for i, name := range []string{"read_file", "run_command"} {
		start, end := tools[2*i], tools[2*i+1]
		if start.Type != core.EventToolUse || start.ToolName != name || start.ToolInput == "" {
			t.Fatalf("start=%+v", start)
		}
		if end.Type != core.EventToolResult || end.ToolName != name || end.ToolResult == "" {
			t.Fatalf("end=%+v", end)
		}
	}
	if !strings.Contains(tools[0].ToolInput, "/workspace/probe.txt") || !strings.Contains(tools[2].ToolInput, "pwd") {
		t.Fatal("missing tool arguments")
	}
	if text != "DONE\n" {
		t.Fatalf("final text duplicated or polluted: %q", text)
	}
}

func TestStream_ToolSnapshotsDoNotDuplicateAndKeepSeparateCalls(t *testing.T) {
	s := &antigravitySession{ctx: context.Background(), events: make(chan core.Event, 32)}
	active := `{"event":"step_update","step_update":{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_info":{"name":"read_file","parameters":{"Path":"a"}}}}` + "\n"
	done := `{"event":"step_update","step_update":{"step_index":0,"state":"DONE","step_type":"tool","tool_info":{"output":"not found"}}}` + "\n"
	second := `{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"tool","tool_name":"read_file","tool_info":{"output":"ok"}}}` + "\n"
	s.readStream(strings.NewReader(active + active + done + done + active + second))
	if len(s.events) != 4 {
		t.Fatalf("got %d events", len(s.events))
	}
	<-s.events
	result := <-s.events
	if result.ToolName != "read_file" || result.ToolResult != "not found" || result.ToolSuccess != nil {
		t.Fatalf("lost failure output or inferred success: %+v", result)
	}
	if e := <-s.events; e.Type != core.EventToolUse {
		t.Fatalf("missing start for done-only snapshot: %+v", e)
	}
}

func TestStream_UnknownStepsAreNotExposedAsToolProgress(t *testing.T) {
	s := &antigravitySession{ctx: context.Background(), events: make(chan core.Event, 32)}
	s.readStream(strings.NewReader(`{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"step_index":2,"state":"FUTURE_STATE","step_type":"tool","tool_name":"read_file"}}
{"event":"step_update","step_update":{"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}}
`))
	if len(s.events) != 0 {
		t.Fatalf("unexpected events: %d", len(s.events))
	}
}

func TestStream_ToolFailuresAreShownWithoutFailingTheConversation(t *testing.T) {
	data, err := os.ReadFile("testdata/tool-failure-stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	s := &antigravitySession{ctx: context.Background(), events: make(chan core.Event, 32)}
	s.readStream(strings.NewReader(string(data)))
	var results []core.Event
	for len(s.events) > 0 {
		e := <-s.events
		if e.Type == core.EventError {
			t.Fatalf("tool failure aborted conversation: %+v", e)
		}
		if e.Type == core.EventToolResult {
			results = append(results, e)
		}
	}
	if len(results) != 2 {
		t.Fatalf("results=%+v", results)
	}
	for _, e := range results {
		if e.ToolStatus != "failed" || e.ToolSuccess == nil || *e.ToolSuccess {
			t.Fatalf("failure shown as success: %+v", e)
		}
	}
	if results[0].ToolExitCode == nil || *results[0].ToolExitCode != 1 {
		t.Fatalf("lost exit code: %+v", results[0])
	}
	if results[1].ToolResult != "file does not exist" {
		t.Fatalf("lost tool error: %+v", results[1])
	}
}
