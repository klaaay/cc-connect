package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func managedPost(t *testing.T, e *Engine, body managedDeliveryRequest) (*httptest.ResponseRecorder, managedDeliveryReceipt) {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/test/sessions/deliver", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	(&ManagementServer{}).handleManagedDelivery(w, r, e)
	var envelope struct {
		Data managedDeliveryReceipt `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return w, envelope.Data
}
func TestManagedDelivery_PinsIdentityAndPersistsDeduplication(t *testing.T) {
	platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
	agentSession := newResultAgentSession("done")
	path := filepath.Join(t.TempDir(), "sessions.json")
	engine := NewEngine("test", &resultAgent{session: agentSession}, []Platform{platform}, path, LangEnglish)
	selected := engine.sessions.GetOrCreateActive("telegram:1:1")
	request := managedDeliveryRequest{SessionKey: "telegram:1:1", SessionID: selected.ID, RequestID: "answer-1", Prompt: "bound answer"}
	first, receipt := managedPost(t, engine, request)
	if first.Code != 200 || receipt.Status != "accepted" {
		t.Fatalf("first delivery: %d %s", first.Code, first.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for len(platform.getSent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(platform.getSent()) != 1 {
		t.Fatal("agent did not reply")
	}
	duplicate, again := managedPost(t, engine, request)
	if duplicate.Code != 200 || again != receipt {
		t.Fatalf("duplicate: %s", duplicate.Body.String())
	}
	restored := NewEngine("test", &stubAgent{}, []Platform{platform}, path, LangEnglish)
	restored.sessions.NewSession(request.SessionKey, "next")
	_, afterRestart := managedPost(t, restored, request)
	if afterRestart != receipt {
		t.Fatal("receipt lost after restart / session replacement")
	}
	request.Prompt = "changed"
	mismatch, _ := managedPost(t, restored, request)
	if mismatch.Code != 409 {
		t.Fatalf("identity change accepted: %d", mismatch.Code)
	}
	request.RequestID = "answer-2"
	_, stale := managedPost(t, restored, request)
	if stale.Status != "rejected:session_changed" {
		t.Fatalf("old answer routed to new session: %#v", stale)
	}
}
func TestManagedDelivery_UnknownIntentNeverResends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	engine := NewEngine("test", &stubAgent{}, nil, path, LangEnglish)
	request := managedDeliveryRequest{SessionKey: "test:1", SessionID: "s1", RequestID: "lost", Prompt: "must not resend"}
	raw, _ := json.Marshal(request)
	receipt := managedDeliveryReceipt{Fingerprint: digestManaged(raw), RequestID: request.RequestID, SessionID: request.SessionID, Status: "unknown"}
	if err := saveManagedReceipt(filepath.Join(path+".deliveries", digestManaged([]byte(request.RequestID))+".json"), receipt); err != nil {
		t.Fatal(err)
	}
	_, restored := managedPost(t, engine, request)
	if restored.Status != "unknown" {
		t.Fatal("uncertain delivery was retried")
	}
}

func TestManagedDelivery_LookupDoesNotCreateDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	engine := NewEngine("test", &stubAgent{}, nil, path, LangEnglish)
	request := managedDeliveryRequest{SessionKey: "test:1", SessionID: "s1", RequestID: "lookup", Prompt: "must not send", LookupOnly: true}
	response, receipt := managedPost(t, engine, request)
	if response.Code != 200 || receipt.Status != "not_found" {
		t.Fatalf("lookup: %s", response.Body.String())
	}
	if _, err := os.Stat(path + ".deliveries"); !os.IsNotExist(err) {
		t.Fatal("read-only lookup created receipt storage")
	}
	request.LookupOnly = false
	raw, _ := json.Marshal(request)
	receipt.Status = "accepted"
	receipt.Fingerprint = digestManaged(raw)
	if err := saveManagedReceipt(filepath.Join(path+".deliveries", digestManaged([]byte(request.RequestID))+".json"), receipt); err != nil {
		t.Fatal(err)
	}
	request.LookupOnly = true
	_, restored := managedPost(t, engine, request)
	if restored != receipt {
		t.Fatal("lookup changed accepted receipt")
	}
}

func TestManagedDelivery_GuardedNewRequiresSameIdleSession(t *testing.T) {
	platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
	engine := NewEngine("test", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	selected := engine.sessions.GetOrCreateActive("telegram:1:1")
	previous := selected.ID
	guard := webhookIngressGuard{previousSessionID: &previous}
	if !selected.TryLock() {
		t.Fatal("test session not idle")
	}
	delivery, _ := (&WebhookServer{}).acceptPrompt(engine, "telegram:1:1", "/new isolated", true, "test", guard)
	if delivery != "rejected:session_busy" || engine.sessions.ActiveSessionID("telegram:1:1") != previous {
		t.Fatal("busy session was replaced")
	}
	selected.Unlock()
	replacement := engine.sessions.NewSession("telegram:1:1", "user-created")
	delivery, _ = (&WebhookServer{}).acceptPrompt(engine, "telegram:1:1", "/new isolated", true, "test", guard)
	if delivery != "rejected:session_changed" || engine.sessions.ActiveSessionID("telegram:1:1") != replacement.ID {
		t.Fatal("stale request replaced a different session")
	}
	previous = replacement.ID
	_, _ = (&WebhookServer{}).acceptPrompt(engine, "telegram:1:1", "/new isolated", true, "test", guard)
	if engine.sessions.ActiveSessionID("telegram:1:1") == previous {
		t.Fatal("matching idle session did not reset")
	}
}

func TestManagedDelivery_RejectsSameIDInDifferentWorkspace(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "new"}[reset], func(t *testing.T) {
			platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
			e := NewEngine("test", &stubAgent{}, []Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
			key := "telegram:1:1"
			primary := e.sessions.GetOrCreateActive(key)
			directory := t.TempDir()
			e.workspacePool = newWorkspacePool(DefaultWorkspaceIdleTimeout)
			ws := e.workspacePool.GetOrCreate(directory)
			ws.agent = &stubAgent{}
			ws.sessions = NewSessionManager("")
			other := ws.sessions.GetOrCreateActive(key)
			if primary.ID != other.ID {
				t.Fatal("fixture must exercise colliding session IDs")
			}
			e.bindSendWorkDir(key, directory)
			guard := webhookIngressGuard{sessionID: primary.ID}
			prompt := "answer for primary workspace"
			if reset {
				guard = webhookIngressGuard{previousSessionID: &primary.ID}
				prompt = "/new primary-task"
			}
			delivery, _ := (&WebhookServer{}).acceptPrompt(e, key, prompt, true, "test", guard)
			if delivery != "rejected:session_changed" {
				t.Fatalf("cross-workspace request admitted: %s", delivery)
			}
			if e.sessions.ActiveSessionID(key) != primary.ID || ws.sessions.ActiveSessionID(key) != other.ID {
				t.Fatal("rejection changed selected session")
			}
			if len(other.GetHistory(10)) != 0 {
				t.Fatal("answer leaked into other workspace")
			}
		})
	}
}

func TestManagedDelivery_NewPreservesOriginalTaskHistory(t *testing.T) {
	platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
	path := filepath.Join(t.TempDir(), "sessions.json")
	engine := NewEngine("test", &stubAgent{}, []Platform{platform}, path, LangEnglish)
	key := "telegram:1:1"
	old := engine.sessions.GetOrCreateActive(key)
	old.SetAgentSessionID("original-agent", "codex")
	old.AddHistory("user", "FIX_DISPATCH_TASK original\n任务正文")
	old.AddHistory("user", "补充要求：保留 Web 行为")
	previous := old.ID
	_, _ = (&WebhookServer{}).acceptPrompt(engine, key, "/new next-task", true, "test", webhookIngressGuard{previousSessionID: &previous})
	fresh := engine.sessions.GetOrCreateActive(key)
	if fresh.ID == previous || fresh.HistoryLen() != 0 || fresh.GetAgentSessionID() != "" {
		t.Fatal("新任务未获得独立空会话")
	}
	if old.HistoryLen() != 2 || old.GetHistory(0)[1].Content != "补充要求：保留 Web 行为" {
		t.Fatal("新建任务清除了原任务需求证据")
	}
	restored := NewSessionManager(path)
	archived, err := restored.SwitchSession(key, previous)
	if err != nil || archived.HistoryLen() != 2 {
		t.Fatal("重启后原任务历史丢失")
	}
}
