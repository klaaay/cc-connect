package antigravity

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/chenhg5/cc-connect/agent/antigravityhook"
	"github.com/chenhg5/cc-connect/core"
)

func (as *antigravitySession) SteerTurn(prompt string) error {
	if !as.Alive() || as.steeringBridge == nil {
		return errors.New("antigravity: no active turn")
	}
	return as.steeringBridge.steer(prompt)
}
func (b *agyPermissionBridge) beginTurn() {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	b.turnActive = true
}
func (b *agyPermissionBridge) steer(prompt string) error {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	if !b.turnActive || b.ctx.Err() != nil {
		return errors.New("antigravity: no active turn")
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("antigravity: empty guidance")
	}
	total := len(prompt)
	for _, v := range b.guidance {
		total += len(v)
	}
	if len(b.guidance) >= 32 || len(prompt) > 16384 || total > 32768 {
		return errors.New("antigravity: guidance queue limit reached")
	}
	b.guidance = append(b.guidance, prompt)
	return nil
}
func (b *agyPermissionBridge) invocationHook(event string) antigravityhook.BridgeResponse {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	response := antigravityhook.BridgeResponse{}
	if event == "Stop" {
		// Stop 之后 AGY 仍可能继续调用工具。由流的 result 或进程退出结束本轮。
		if len(b.guidance) > 0 {
			response.Decision = "continue"
			response.Reason = "User guidance is pending in the next invocation."
		}
		return response
	}
	for _, prompt := range b.guidance {
		response.InjectSteps = append(response.InjectSteps, antigravityhook.InjectStep{UserMessage: prompt})
	}
	b.guidance = nil
	if event == "PostInvocation" && len(response.InjectSteps) > 0 {
		response.TerminationBehavior = "force_continue"
	}
	return response
}
func (b *agyPermissionBridge) endTurn() {
	b.turnMu.Lock()
	pending := len(b.guidance) > 0
	b.guidance = nil
	b.turnActive = false
	b.turnMu.Unlock()
	if pending {
		select {
		case b.events <- core.Event{Type: core.EventError, Error: errors.New("antigravity: turn ended before queued guidance could be delivered")}:
		case <-b.ctx.Done():
		}
	}
}

func (*antigravitySession) SteeringDeferred() bool { return true }

func (b *agyPermissionBridge) invocationHookFor(event string, data json.RawMessage) antigravityhook.BridgeResponse {
	var input struct {
		ConversationID string `json:"conversationId"`
	}
	if json.Unmarshal(data, &input) != nil || input.ConversationID == "" {
		return antigravityhook.BridgeResponse{}
	}
	b.turnMu.Lock()
	if b.conversationID == "" {
		b.conversationID = input.ConversationID
	}
	matches := b.conversationID == input.ConversationID
	b.turnMu.Unlock()
	if !matches {
		return antigravityhook.BridgeResponse{}
	}
	return b.invocationHook(event)
}
