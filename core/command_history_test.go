package core

import (
	"strings"
	"testing"
)

func TestBuiltinControlInputIsObservedWithoutArguments(t *testing.T) {
	e := newTestEngine()
	key := "test:control"
	session := e.sessions.GetOrCreateActive(key)
	p := &stubPlatformEngine{n: "test"}
	if !e.handleCommand(p, &Message{SessionKey: key, InputOrigin: "human", UserID: "user"}, "/name private-argument") {
		t.Fatal("command not consumed")
	}
	history := session.GetHistory(0)
	if len(history) != 1 || history[0].Origin != "human" || history[0].Content != "CC_CONNECT_CONTROL /name" {
		t.Fatalf("missing control fact: %+v", history)
	}
	if strings.Contains(history[0].Content, "private-argument") {
		t.Fatal("command arguments must not enter observation")
	}
	e.handleCommand(p, &Message{SessionKey: key, InputOrigin: "automation"}, "/name auto")
	if len(session.GetHistory(0)) != 1 {
		t.Fatal("automated control is not a human action")
	}
	e.handleCommand(p, &Message{SessionKey: key}, "/name old")
	history = session.GetHistory(0)
	if len(history) != 2 || history[1].Origin != "" {
		t.Fatal("unknown input must remain unknown")
	}
}

func TestControlQueriesDoNotCountAsActions(t *testing.T) {
	for _, raw := range []string{"version", "usage", "skills", "history", "model", "mode", "provider", "provider list", "provider current", "config", "config get display", "cron list", "timer list", "commands list", "alias list", "heartbeat status", "memory show", "memory global show"} {
		parts := strings.Fields(raw)
		if changesExecutionContext(parts[0], parts[1:]) {
			t.Errorf("query recorded as action: %s", raw)
		}
	}
	for _, raw := range []string{"new", "stop", "shell", "mode yolo", "model example", "config set display compact", "cron add example", "quiet", "memory add example"} {
		parts := strings.Fields(raw)
		if !changesExecutionContext(parts[0], parts[1:]) {
			t.Errorf("action omitted: %s", raw)
		}
	}
}

func TestLegacyCardActionPreservesUnknownProvenance(t *testing.T) {
	e := newTestEngine()
	key := "test:card"
	session := e.sessions.GetOrCreateActive(key)
	e.handleCardNav("nav:/mode", key)
	if session.HistoryLen() != 0 {
		t.Fatal("card navigation must not record participation")
	}
	e.handleCardNav("act:/mode yolo", key)
	history := session.GetHistory(0)
	if len(history) != 1 || history[0].Origin != "" || history[0].Content != "CC_CONNECT_CONTROL /mode" {
		t.Fatalf("legacy card lost unknown origin: %+v", history)
	}
}
