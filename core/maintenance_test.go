package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMaintenance_PersistsAndRequiresOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance.json")
	g, err := OpenMaintenance(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := g.Enter()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Begin("operation-1", "revision-1"); err == nil {
		t.Fatal("busy accepted")
	}
	release()
	if err := g.Begin("operation-1", "revision-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Enter(); err == nil {
		t.Fatal("execution admitted")
	}
	if err := g.Begin("operation-1", "revision-2"); err == nil {
		t.Fatal("changed request accepted")
	}
	restored, err := OpenMaintenance(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Enter(); err == nil {
		t.Fatal("restart lost maintenance")
	}
	if err := restored.End("other"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if err := restored.End("operation-1"); err != nil {
		t.Fatal(err)
	}
	done, err := restored.Enter()
	if err != nil {
		t.Fatal(err)
	}
	done()
}

func TestMaintenance_ConcurrentAdmission(t *testing.T) {
	for i := 0; i < 100; i++ {
		g, err := OpenMaintenance(filepath.Join(t.TempDir(), "maintenance.json"))
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		release := make(chan struct{})
		entered := make(chan bool, 1)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			done, err := g.Enter()
			entered <- err == nil
			if err == nil {
				<-release
				done()
			}
		}()
		close(start)
		err = g.Begin("operation-1", "revision-1")
		ok := <-entered
		if err == nil && ok {
			t.Fatal("maintenance and execution both admitted")
		}
		close(release)
		wg.Wait()
	}
}

func TestMaintenance_SessionLifetime(t *testing.T) {
	g, err := OpenMaintenance(filepath.Join(t.TempDir(), "maintenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	InstallMaintenance(g)
	defer InstallMaintenance(nil)
	session := &Session{}
	if !session.TryLock() {
		t.Fatal("initial lock")
	}
	if g.Begin("op", "rev") == nil {
		t.Fatal("busy session missed")
	}
	session.Unlock()
	if err := g.Begin("op", "rev"); err != nil {
		t.Fatal(err)
	}
	if session.TryLock() {
		t.Fatal("session bypassed maintenance")
	}
}

func TestMaintenance_CorruptStateFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for _, data := range []string{`{}`, `{"phase":"paused"}`, `{"phase":"released","operation_id":"op"}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenMaintenance(path); err == nil {
			t.Fatal("corrupt state accepted")
		}
	}
}
func TestMaintenance_RestartAttemptsAreNotReplayed(t *testing.T) {
	g, err := OpenMaintenance(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Begin("op", "revision"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"apply", "rollback"} {
		first, err := g.Restart("op", id)
		if err != nil || !first {
			t.Fatal("first restart rejected")
		}
	}
	if first, err := g.Restart("op", "apply"); err != nil || first {
		t.Fatal("old restart replayed")
	}
	if _, err := g.Restart("op", "apply", "changed"); err == nil {
		t.Fatal("changed restart request accepted")
	}
	if err := g.End("op"); err != nil {
		t.Fatal(err)
	}
	if err := g.Begin("other", "revision"); err != nil {
		t.Fatal(err)
	}
	if err := g.End("other"); err != nil {
		t.Fatal(err)
	}
	if err := g.Begin("op", "revision"); err == nil {
		t.Fatal("completed operation replayed")
	}
}
func TestMaintenance_ExecutionEntrypoints(t *testing.T) {
	g, err := OpenMaintenance(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	InstallMaintenance(g)
	defer InstallMaintenance(nil)
	if err := g.Begin("op", "revision"); err != nil {
		t.Fatal(err)
	}
	e := NewEngine("fixture", &stubAgent{}, nil, "", LangChinese)
	for _, err := range []error{e.ExecuteCronJob(nil), e.ExecuteTimerJob(nil), e.ExecuteHeartbeat("", "", false)} {
		if !errors.Is(err, ErrMaintenance) {
			t.Fatal("scheduler bypassed maintenance")
		}
	}
	if _, err := e.HandleRelay(context.Background(), "source", "session", "prompt"); !errors.Is(err, ErrMaintenance) {
		t.Fatal("relay bypassed maintenance")
	}
	if _, err := (&WebhookServer{}).acceptPrompt(e, "", "", false, ""); !errors.Is(err, ErrMaintenance) {
		t.Fatal("webhook bypassed maintenance")
	}
}

func TestMaintenance_WebhookShellIsRejectedBeforeAcceptance(t *testing.T) {
	g, err := OpenMaintenance(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	InstallMaintenance(g)
	defer InstallMaintenance(nil)
	if err := g.Begin("op", "revision"); err != nil {
		t.Fatal(err)
	}
	ws := NewWebhookServer(0, "", "/hook")
	ws.RegisterEngine("fixture", NewEngine("fixture", &stubAgent{}, nil, "", LangChinese))
	result := postWebhookPrompt(t, ws, WebhookRequest{Project: "fixture", SessionKey: "fixture:user", Exec: "exit 0"})
	if result.Code != 409 {
		t.Fatalf("shell accepted during maintenance: %d", result.Code)
	}
}
