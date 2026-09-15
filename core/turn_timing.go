package core

import "time"

// TurnTiming records a completed agent turn measured by the execution host.
// Queued time and gaps between turns are outside these boundaries.
// Missing timing (old history, interrupted turns) must remain unknown.
type TurnTiming struct {
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
	ExecutionMs      int64     `json:"execution_ms"`
	PermissionWaitMs int64     `json:"permission_wait_ms"`
}

func completedTurnTiming(startedAt, endedAt time.Time, permissionWait time.Duration) *TurnTiming {
	elapsed := endedAt.Sub(startedAt)
	if startedAt.IsZero() || elapsed < 0 || permissionWait < 0 || permissionWait > elapsed {
		return nil
	}
	return &TurnTiming{StartedAt: startedAt, EndedAt: endedAt, ExecutionMs: (elapsed - permissionWait).Milliseconds(), PermissionWaitMs: permissionWait.Milliseconds()}
}

func (s *Session) AddAssistantHistoryWithTiming(content string, timing *TurnTiming) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var copied *TurnTiming
	if timing != nil {
		value := *timing
		copied = &value
	}
	s.History = append(s.History, HistoryEntry{Role: "assistant", Content: content, Timestamp: time.Now(), Timing: copied})
}
