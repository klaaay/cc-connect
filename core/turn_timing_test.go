package core

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestTurnTimingSeparatesPermissionWait(t *testing.T) {
	started := time.Now()
	timing := completedTurnTiming(started, started.Add(9*time.Second), 4*time.Second)
	if timing == nil || timing.ExecutionMs != 5000 || timing.PermissionWaitMs != 4000 {
		t.Fatalf("unexpected timing: %+v", timing)
	}
	if !timing.StartedAt.Equal(started) || !timing.EndedAt.Equal(started.Add(9*time.Second)) {
		t.Fatal("measurement boundaries changed")
	}
}

func TestTurnTimingDoesNotInventUnknownOrInvalidIntervals(t *testing.T) {
	now := time.Now()
	for _, timing := range []*TurnTiming{
		completedTurnTiming(time.Time{}, now, 0),
		completedTurnTiming(now, now.Add(-time.Second), 0),
		completedTurnTiming(now, now.Add(time.Second), 2*time.Second),
		completedTurnTiming(now, now.Add(time.Second), -time.Second),
	} {
		if timing != nil {
			t.Fatalf("invalid interval accepted: %+v", timing)
		}
	}
}

func TestAssistantTimingIsRecordedAlongsideOneHistoryEntry(t *testing.T) {
	session := &Session{}
	now := time.Now()
	timing := completedTurnTiming(now.Add(-time.Second), now, 0)
	session.AddAssistantHistoryWithTiming("result", timing)
	history := session.GetHistory(0)
	if len(history) != 1 || history[0].Role != "assistant" || history[0].Timing == nil || history[0].Timing.ExecutionMs != 1000 {
		t.Fatalf("history timing absent: %+v", history)
	}
	session.AddHistory("assistant", "legacy")
	if session.GetHistory(0)[1].Timing != nil {
		t.Fatal("legacy timing must remain unknown")
	}
}

func TestInteractiveTimingExcludesPermissionPauseAndPreSendTime(t *testing.T) {
	platform := &stubMediaPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	engine := NewEngine("timing", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(func() { _ = engine.Stop() })
	session := engine.sessions.GetOrCreateActive("test:timing")
	agent := newControllableSession("timing-agent")
	state := &interactiveState{agentSession: agent, platform: platform, replyCtx: "ctx"}
	engine.interactiveStates["test:timing"] = state
	agent.events <- Event{Type: EventPermissionRequest, RequestID: "permission-timing", ToolName: "Bash"}
	finished := make(chan struct{})
	started := time.Now()
	go func() {
		engine.processInteractiveEvents(state, session, engine.sessions, "test:timing", "timed", started.Add(-time.Hour), nil, nil, "ctx", started)
		close(finished)
	}()
	deadline := time.Now().Add(2 * time.Second)
	var pending *pendingPermission
	for pending == nil && time.Now().Before(deadline) {
		state.mu.Lock()
		pending = state.pending
		state.mu.Unlock()
		if pending == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if pending == nil {
		t.Fatal("permission not received")
	}
	time.Sleep(80 * time.Millisecond)
	close(pending.Resolved)
	agent.events <- Event{Type: EventResult, Content: "done", Done: true}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish")
	}
	history := session.GetHistory(0)
	timing := history[len(history)-1].Timing
	if timing == nil || timing.PermissionWaitMs < 75 || timing.ExecutionMs > 1000 {
		t.Fatalf("incorrect measured interval: %+v", timing)
	}
	if !timing.StartedAt.Equal(started) {
		t.Fatal("included pre-send time")
	}
}

type timingFinalizeSession struct{ *controllableAgentSession }

func (s *timingFinalizeSession) CurrentSessionID() string {
	time.Sleep(80 * time.Millisecond)
	return "timing-finalize"
}
func TestTurnTimingEndsBeforeResultFinalization(t *testing.T) {
	platform := &stubMediaPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	engine := NewEngine("timing", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(func() { _ = engine.Stop() })
	session := engine.sessions.GetOrCreateActive("test:finalize")
	agent := &timingFinalizeSession{newControllableSession("timing-finalize")}
	state := &interactiveState{agentSession: agent, platform: platform, replyCtx: "ctx"}
	agent.events <- Event{Type: EventResult, Content: "done", Done: true}
	started := time.Now()
	engine.processInteractiveEvents(state, session, engine.sessions, "test:finalize", "timed", started, nil, nil, "ctx", started)
	timing := session.GetHistory(0)[0].Timing
	if timing == nil || time.Since(timing.EndedAt) < 75*time.Millisecond {
		t.Fatalf("finalization included in timing: %+v", timing)
	}
}

type timingDrainSession struct{ *controllableAgentSession }

func (s *timingDrainSession) Send(prompt, messageID string, images []ImageAttachment, files []FileAttachment) error {
	s.events <- Event{Type: EventResult, Content: "done", Done: true}
	return nil
}
func TestTurnTimingCoversDrainAndResetsQueuedTurn(t *testing.T) {
	platform := &stubMediaPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	engine := NewEngine("timing", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	t.Cleanup(func() { _ = engine.Stop() })
	session := engine.sessions.GetOrCreateActive("test:drain")
	agent := &timingDrainSession{newControllableSession("timing-drain")}
	state := &interactiveState{agentSession: agent, platform: platform, replyCtx: "ctx", pendingMessages: []queuedMessage{
		{content: "first", platform: platform, replyCtx: "ctx", messageID: "one"},
		{content: "second", platform: platform, replyCtx: "ctx", messageID: "two"},
	}}
	engine.drainPendingMessages(state, session, engine.sessions, "test:drain")
	var timings []*TurnTiming
	for _, entry := range session.GetHistory(0) {
		if entry.Role == "assistant" {
			if entry.Timing == nil {
				t.Fatal("queued turn missing timing")
			}
			timings = append(timings, entry.Timing)
		}
	}
	if len(timings) != 2 || timings[1].StartedAt.Before(timings[0].EndedAt) {
		t.Fatalf("invalid queued intervals: %+v", timings)
	}
}

func TestTurnTimingSurvivesRestartAndManagementResponse(t *testing.T) {
	_, server, engine := testManagementServer(t, "tok")
	path := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(path)
	session := manager.GetOrCreateActive("test:timing")
	start := time.Now().Add(-9 * time.Second)
	session.AddAssistantHistoryWithTiming("done", completedTurnTiming(start, start.Add(9*time.Second), 4*time.Second))
	session.AddHistory("assistant", "legacy")
	manager.Save()
	engine.sessions = NewSessionManager(path)
	response := mgmtGet(t, server.URL+"/api/v1/projects/test-project/sessions/"+session.ID, "tok")
	if !response.OK {
		t.Fatalf("management failed: %s", response.Error)
	}
	var data struct {
		History []struct {
			Timing *TurnTiming `json:"timing"`
		} `json:"history"`
	}
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.History) != 2 || data.History[0].Timing == nil || data.History[0].Timing.ExecutionMs != 5000 || data.History[0].Timing.PermissionWaitMs != 4000 || data.History[1].Timing != nil {
		t.Fatalf("persisted timing lost: %+v", data)
	}
}
