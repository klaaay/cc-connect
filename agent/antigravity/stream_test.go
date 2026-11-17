package antigravity

import (
	"context"
	"github.com/chenhg5/cc-connect/core"
	"strings"
	"testing"
)

func TestStream_ReportsConversationIDAndDoesNotDuplicateFinalResponse(t *testing.T) {
	s := &antigravitySession{ctx: context.Background(), events: make(chan core.Event, 10)}
	input := `{"event":"init","conversation_id":"agy-123"}
{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"OK"}}
{"event":"result","result":{"conversation_id":"agy-123","status":"SUCCESS","response":"OK"}}
`
	s.readStream(strings.NewReader(input))
	if s.CurrentSessionID() != "agy-123" {
		t.Fatal(s.CurrentSessionID())
	}
	var text string
	for len(s.events) > 0 {
		e := <-s.events
		if e.Type == core.EventText {
			text += e.Content
		}
	}
	if text != "OK" {
		t.Fatalf("text=%q", text)
	}
}
