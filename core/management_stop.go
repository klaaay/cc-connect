package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// managedSessionStop keeps an idempotent result so retrying an old cancellation
// cannot stop a newer turn in the same conversation. Protected by interactiveMu
// until done closes; readers of err must first wait for done.
type managedSessionStop struct {
	sessionKey string
	sessionID  string
	done       chan struct{}
	err        error
}

type managedStopRequest struct {
	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	RequestID  string `json:"request_id"`
}

var errManagedStopPending = errors.New("session teardown not yet confirmed")

func (m *ManagementServer) handleProjectSessionStop(w http.ResponseWriter, r *http.Request, e *Engine) {
	if r.Method != http.MethodPost {
		mgmtError(w, http.StatusMethodNotAllowed, e.i18n.T(MsgManagedStopInvalid))
		return
	}
	var body managedStopRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil ||
		strings.TrimSpace(body.SessionKey) == "" || strings.TrimSpace(body.SessionID) == "" || strings.TrimSpace(body.RequestID) == "" {
		mgmtError(w, http.StatusBadRequest, e.i18n.T(MsgManagedStopInvalid))
		return
	}
	stop, err := e.beginManagedSessionStop(body)
	if err != nil {
		if errors.Is(err, errManagedStopPending) {
			mgmtJSON(w, http.StatusAccepted, map[string]any{"stopped": false, "request_id": body.RequestID})
			return
		}
		mgmtError(w, http.StatusConflict, e.i18n.T(MsgManagedStopConflict))
		return
	}
	// A bounded long poll lets clients observe progress without assuming that
	// dispatching the stop signal means the subprocess has exited.
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-stop.done:
		if stop.err != nil {
			mgmtError(w, http.StatusBadGateway, e.i18n.T(MsgSessionCloseFailed))
			return
		}
		mgmtJSON(w, http.StatusOK, map[string]any{"stopped": true, "request_id": body.RequestID})
	case <-timer.C:
		mgmtJSON(w, http.StatusAccepted, map[string]any{"stopped": false, "request_id": body.RequestID})
	case <-r.Context().Done():
	}
}

func (e *Engine) beginManagedSessionStop(input managedStopRequest) (*managedSessionStop, error) {
	e.managedInputMu.Lock()
	defer e.managedInputMu.Unlock()
	key := e.interactiveKeyForSessionKey(input.SessionKey)
	// The management session API currently exposes the project's own manager.
	// Reject a different workspace rather than stopping a similarly named session.
	if key != input.SessionKey {
		return nil, errors.New("workspace session is not exposed by management")
	}
	e.interactiveMu.Lock()
	defer e.interactiveMu.Unlock()
	if old := e.managedSessionStops[input.RequestID]; old != nil {
		if old.sessionKey != key || old.sessionID != input.SessionID {
			return nil, errors.New("request identity changed")
		}
		return old, nil
	}
	if e.activeManagedStops[key] != nil {
		return nil, errors.New("session changed or another stop is pending")
	}
	e.closingMu.Lock()
	closing := e.closingSessions[key] != nil || e.unsafeResume[key]
	e.closingMu.Unlock()
	if closing {
		return nil, errManagedStopPending
	}
	session := e.sessions.FindByID(input.SessionID)
	idToKey, _ := e.sessions.SessionKeyMap()
	if session == nil || idToKey[input.SessionID] != input.SessionKey {
		return nil, errors.New("session not found")
	}
	stop := &managedSessionStop{sessionKey: key, sessionID: input.SessionID, done: make(chan struct{})}
	if e.sessions.ActiveSessionID(input.SessionKey) != input.SessionID {
		// A replaced session can be acknowledged only when its event loop and
		// all teardown gates have settled. Never stop the current replacement.
		if session.Busy() || e.interactiveStates[key] != nil {
			return nil, errManagedStopPending
		}
		if e.managedSessionStops == nil {
			e.managedSessionStops = make(map[string]*managedSessionStop)
		}
		e.managedSessionStops[input.RequestID] = stop
		close(stop.done)
		return stop, nil
	}
	if e.managedSessionStops == nil {
		e.managedSessionStops = make(map[string]*managedSessionStop)
	}
	if e.activeManagedStops == nil {
		e.activeManagedStops = make(map[string]*managedSessionStop)
	}
	e.managedSessionStops[input.RequestID] = stop
	e.activeManagedStops[key] = stop
	state := e.interactiveStates[key]
	var agentSession AgentSession
	if state != nil {
		state.markStopped()
		state.mu.Lock()
		agentSession = state.agentSession
		pending := state.pending
		state.pending = nil
		state.pendingMessages = nil
		if state.agentSessionIdleCancel != nil {
			state.agentSessionIdleCancel()
			state.agentSessionIdleCancel = nil
		}
		state.mu.Unlock()
		if pending != nil {
			pending.resolve()
		}
	}
	finish := e.beginSessionClose(key)
	go e.finishManagedSessionStop(key, session, state, agentSession, stop, finish)
	return stop, nil
}

func (e *Engine) finishManagedSessionStop(key string, session *Session, state *interactiveState, agent AgentSession, stop *managedSessionStop, finish func()) {
	defer close(stop.done)
	defer finish()
	if state != nil {
		e.stopUnsolicitedReader(state)
	}
	// Close errors and timeouts retain the gate: the process might still be live.
	stop.err = e.closeAgentSession(key, agent, nil, nil)
	if stop.err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, closeTimeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for session.Busy() {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			stop.err = fmt.Errorf("stop: waiting for event loop: %w", ctx.Err())
			return
		}
	}
	e.interactiveMu.Lock()
	if e.interactiveStates[key] == state {
		delete(e.interactiveStates, key)
	}
	delete(e.activeManagedStops, key)
	e.interactiveMu.Unlock()
	e.hooks.Emit(HookEvent{Event: HookEventSessionEnded, SessionKey: key})
}
