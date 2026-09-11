package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func requestManagedStop(m *ManagementServer, sessionID string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"session_key": "test:user1", "session_id": sessionID, "request_id": "stop-task-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/test-project/sessions/stop", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	out := httptest.NewRecorder()
	m.wrap(m.handleProjectRoutes)(out, req)
	return out
}

func TestManagementStopWaitsForCloseAndPreservesHistory(t *testing.T) {
	m, _, e := testManagementServer(t, "secret")
	defer e.cancel()
	session := e.sessions.GetOrCreateActive("test:user1")
	session.AddHistory("user", "fix task")
	agent := newBlockingCloseSession("agent")
	state := &interactiveState{agentSession: agent, pendingMessages: []queuedMessage{{content: "queued task"}}}
	e.interactiveStates["test:user1"] = state
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- requestManagedStop(m, session.ID) }()
	select {
	case <-agent.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("close not started")
	}
	select {
	case <-result:
		t.Fatal("acknowledged before process stopped")
	case <-time.After(20 * time.Millisecond):
	}
	if !state.isStopped() {
		t.Fatal("event loop not stopped")
	}
	state.mu.Lock()
	queued := len(state.pendingMessages)
	state.mu.Unlock()
	if queued != 0 {
		t.Fatal("queued messages not cleared")
	}
	close(agent.releaseClose)
	select {
	case r := <-result:
		if r.Code != 200 {
			t.Fatalf("response %d: %s", r.Code, r.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
	if e.sessions.ActiveSessionID("test:user1") != session.ID {
		t.Fatal("stop must preserve the conversation")
	}
	if len(session.GetHistory(10)) != 1 {
		t.Fatal("history lost")
	}
	// A retry of the old request must not stop a later task.
	replacement := newControllableSession("replacement")
	e.interactiveMu.Lock()
	e.interactiveStates["test:user1"] = &interactiveState{agentSession: replacement}
	e.interactiveMu.Unlock()
	if r := requestManagedStop(m, session.ID); r.Code != 200 {
		t.Fatalf("retry: %s", r.Body.String())
	}
	if !replacement.Alive() {
		t.Fatal("old cancel stopped replacement task")
	}
}

func TestManagementStopRejectsWrongSession(t *testing.T) {
	m, _, e := testManagementServer(t, "secret")
	defer e.cancel()
	e.sessions.GetOrCreateActive("test:user1")
	if r := requestManagedStop(m, "unknown"); r.Code != 409 {
		t.Fatalf("want conflict, got %d", r.Code)
	}
}

type failingManagedCloseSession struct{ AgentSession }

func (s *failingManagedCloseSession) Close() error { return errors.New("close failed") }

func TestManagementStopFailureKeepsSpawnBlocked(t *testing.T) {
	m, _, e := testManagementServer(t, "secret")
	defer e.cancel()
	session := e.sessions.GetOrCreateActive("test:user1")
	e.interactiveStates["test:user1"] = &interactiveState{agentSession: &failingManagedCloseSession{}}
	if r := requestManagedStop(m, session.ID); r.Code != 502 {
		t.Fatalf("want failure, got %d", r.Code)
	}
	state := e.getOrCreateInteractiveStateWith("test:user1", nil, nil, session, e.sessions, nil, "")
	if !state.isStopped() {
		t.Fatal("failed close must block new process")
	}
}

func TestManagementStopBeforeAgentStartupDoesNotSpawn(t *testing.T) {
	m, _, e := testManagementServer(t, "secret")
	defer e.cancel()
	session := e.sessions.GetOrCreateActive("test:user1")
	session.TryLock()
	e.interactiveStates["test:user1"] = &interactiveState{}
	stop, err := e.beginManagedSessionStop(managedStopRequest{SessionKey: "test:user1", SessionID: session.ID, RequestID: "stop-task-1"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the accepted processor that had not yet reached agent startup.
	state := e.getOrCreateInteractiveStateWith("test:user1", nil, nil, session, e.sessions, nil, "")
	if !state.isStopped() {
		t.Fatal("cancelled placeholder started an agent")
	}
	session.Unlock()
	select {
	case <-stop.done:
	case <-time.After(time.Second):
		t.Fatal("stop deadlocked with startup")
	}
	if r := requestManagedStop(m, session.ID); r.Code != 200 {
		t.Fatalf("want success: %s", r.Body.String())
	}
}
