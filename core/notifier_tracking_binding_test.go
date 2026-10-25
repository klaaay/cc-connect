package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotifierConfirmationShowsTrackingProject(t *testing.T) {
	var task notifierTask
	if err := json.Unmarshal([]byte(`{"id":"task","label":"分支同步","trackingProject":"clawclaw-hermes-codex-bot"}`), &task); err != nil {
		t.Fatal(err)
	}
	e := &Engine{i18n: NewI18n(LangChinese)}
	text := e.notifierTaskConfirmation(&notifierSelection{task: task})
	if !strings.Contains(text, "clawclaw-hermes-codex-bot") || !strings.Contains(text, "独立") {
		t.Fatalf("missing tracking destination: %s", text)
	}
}
