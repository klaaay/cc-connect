package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

type agyStreamEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	Step           struct {
		ConversationID string `json:"conversation_id"`
		Type           string `json:"step_type"`
		Text           string `json:"text_delta"`
	} `json:"step_update"`
	Result struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
	} `json:"result"`
}

func (as *antigravitySession) emit(event core.Event) {
	select {
	case as.events <- event:
	case <-as.ctx.Done():
	}
}
func (as *antigravitySession) readStream(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	hadText := false
	for scanner.Scan() {
		line := scanner.Text()
		var event agyStreamEvent
		if json.Unmarshal([]byte(line), &event) != nil || event.Event == "" {
			// Keep text wrappers usable, without exposing protocol objects as chat text.
			if !strings.HasPrefix(strings.TrimSpace(line), "{") {
				as.emit(core.Event{Type: core.EventText, Content: line + "\n"})
				hadText = true
			}
			continue
		}
		sid := event.ConversationID
		if sid == "" {
			sid = event.Step.ConversationID
		}
		if sid == "" {
			sid = event.Result.ConversationID
		}
		if sid != "" && sid != as.CurrentSessionID() {
			as.chatID.Store(sid)
			if as.steeringBridge != nil {
				as.steeringBridge.turnMu.Lock()
				as.steeringBridge.conversationID = sid
				as.steeringBridge.turnMu.Unlock()
			}
			as.emit(core.Event{Type: core.EventText, SessionID: sid})
		}
		switch event.Event {
		case "step_update":
			if event.Step.Type == "agent_response" && event.Step.Text != "" {
				hadText = true
				as.emit(core.Event{Type: core.EventText, Content: event.Step.Text})
			}
		case "result":
			if !hadText && event.Result.Response != "" {
				as.emit(core.Event{Type: core.EventText, Content: event.Result.Response})
			}
			if event.Result.Status != "SUCCESS" {
				as.emit(core.Event{Type: core.EventError, Error: fmt.Errorf("antigravity: %s: %s", event.Result.Status, event.Result.Error)})
			}
		}
	}
	if err := scanner.Err(); err != nil && as.ctx.Err() == nil {
		as.emit(core.Event{Type: core.EventError, Error: fmt.Errorf("antigravity: read stream: %w", err)})
	}
}
