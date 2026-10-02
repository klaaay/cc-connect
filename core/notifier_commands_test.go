package core

import (
	"strings"
	"testing"
	"time"
)

func TestNotifierCommandsAreNativeAndNamespaced(t *testing.T) {
	for _, name := range []string{"wn_tasks", "wn_deploy", "wn_runs", "wn_help"} {
		if got := matchPrefix(name, builtinCommands); got != name {
			t.Errorf("/%s is not a native command: %q", name, got)
		}
	}
}

func TestNotifierInputTypes(t *testing.T) {
	for _, tc := range []struct {
		field notifierField
		text  string
		valid bool
	}{
		{notifierField{Type: "number"}, "NaN", false},
		{notifierField{Type: "number"}, "12", true},
		{notifierField{Type: "boolean"}, "2", true},
		{notifierField{Type: "boolean"}, "yes", false},
		{notifierField{Type: "text", Options: []string{"qa", "prod"}}, "2", true},
		{notifierField{Type: "text", Options: []string{"qa", "prod"}}, "3", false},
		{notifierField{Type: "text", Required: true}, "", false},
	} {
		_, err := parseNotifierInput(tc.field, tc.text)
		if (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestNotifierMenusOnlyPublishedForBoundProject(t *testing.T) {
	env := newCUJEnv(t)
	for _, command := range env.engine.GetAllCommands() {
		if strings.HasPrefix(command.Command, "wn_") {
			t.Fatal("unbound menu published")
		}
	}
	_, cfg := newNotifierTestServer(t)
	if err := env.engine.SetNotifier(cfg); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, command := range env.engine.GetAllCommands() {
		if strings.HasPrefix(command.Command, "wn_") {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("got %d notifier commands", count)
	}
}
func TestNotifierTrackingRecoversOriginalDestination(t *testing.T) {
	env := newCUJEnv(t)
	_, cfg := newNotifierTestServer(t)
	if err := env.engine.SetNotifier(cfg); err != nil {
		t.Fatal(err)
	}
	defer env.engine.cancel()
	env.engine.SetDataDir(env.tempDir)
	receipt := notifierReceipt{Run: notifierRun{Kind: "task", ID: "run-1"}, Platform: "test", SessionKey: "test:alice", CreatedAt: time.Now()}
	if err := env.engine.saveNotifierReceipt(receipt, false); err != nil {
		t.Fatal(err)
	}
	if err := env.engine.SetNotifier(cfg); err != nil {
		t.Fatal(err)
	}
	env.engine.resumeNotifierRuns(&cujReplyCtxPlatform{env.plat})
	env.waitFor("recovered result", 8*time.Second, func() bool { return strings.Contains(env.lastSent(), "Succeeded") })
	env.engine.notifier.trackingMu.Lock()
	rows, err := env.engine.notifierReceipts()
	env.engine.notifier.trackingMu.Unlock()
	if err != nil || len(rows) != 0 {
		t.Fatalf("receipt not cleared: %v %v", rows, err)
	}
}
