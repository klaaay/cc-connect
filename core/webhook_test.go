package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type webhookTestPlatform struct {
	stubPlatformEngine
}

func (p *webhookTestPlatform) ReconstructReplyCtx(sessionKey string) (any, error) {
	return sessionKey, nil
}

func postWebhookPrompt(t *testing.T, ws *WebhookServer, request WebhookRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal webhook request: %v", err)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	ws.handleHook(recorder, req)
	return recorder
}

func decodeWebhookResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode webhook response: %v; body=%s", err, recorder.Body.String())
	}
	return response
}

func TestWebhookServer_AuthBearer(t *testing.T) {
	ws := NewWebhookServer(0, "my-secret", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	r.Header.Set("Authorization", "Bearer my-secret")
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with correct Bearer token")
	}
	r.Header.Set("Authorization", "Bearer wrong")
	if ws.authenticate(r) {
		t.Error("expected auth to fail with wrong Bearer token")
	}
}

func TestWebhookServer_AuthHeader(t *testing.T) {
	ws := NewWebhookServer(0, "tok123", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	r.Header.Set("X-Webhook-Token", "tok123")
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with X-Webhook-Token")
	}
}

func TestWebhookServer_AuthQuery(t *testing.T) {
	ws := NewWebhookServer(0, "qsecret", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook?token=qsecret", nil)
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with query token")
	}
}

func TestWebhookServer_NoTokenRequired(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	if !ws.authenticate(r) {
		t.Error("expected auth to pass when no token configured")
	}
}

func TestWebhookServer_HandleHook_MethodNotAllowed(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hook", nil)
	ws.handleHook(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWebhookServer_HandleHook_Unauthorized(t *testing.T) {
	ws := NewWebhookServer(0, "secret", "/hook")
	w := httptest.NewRecorder()
	body, _ := json.Marshal(WebhookRequest{SessionKey: "tg:1:1", Prompt: "hi"})
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	ws.handleHook(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhookServer_HandleHook_Validation(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")

	tests := []struct {
		name string
		body WebhookRequest
		code int
	}{
		{"missing session_key", WebhookRequest{Prompt: "hi"}, http.StatusBadRequest},
		{"missing prompt and exec", WebhookRequest{SessionKey: "tg:1:1"}, http.StatusBadRequest},
		{"both prompt and exec", WebhookRequest{SessionKey: "tg:1:1", Prompt: "hi", Exec: "ls"}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			body, _ := json.Marshal(tt.body)
			r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
			ws.handleHook(w, r)
			if w.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestWebhookServer_DefaultValues(t *testing.T) {
	ws := NewWebhookServer(0, "", "")
	if ws.port != 9111 {
		t.Errorf("expected default port 9111, got %d", ws.port)
	}
	if ws.path != "/hook" {
		t.Errorf("expected default path /hook, got %s", ws.path)
	}
}

func TestWebhookServer_PromptReportsStartedAfterAgentAcceptance(t *testing.T) {
	platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
	agentSession := newResultAgentSession("done")
	engine := NewEngine("fix-project", &resultAgent{session: agentSession}, []Platform{platform}, "", LangEnglish)
	ws := NewWebhookServer(0, "", "/hook")
	ws.RegisterEngine("fix-project", engine)

	recorder := postWebhookPrompt(t, ws, WebhookRequest{
		Project:    "fix-project",
		SessionKey: "telegram:1:1",
		Prompt:     "dispatch-id=started",
		Silent:     true,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeWebhookResponse(t, recorder)
	if response["status"] != "accepted" || response["delivery"] != "started" {
		t.Fatalf("response = %#v, want accepted/started", response)
	}

	deadline := time.Now().Add(time.Second)
	for len(agentSession.sentPrompts) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(agentSession.sentPrompts) != 1 || agentSession.sentPrompts[0] != "dispatch-id=started" {
		t.Fatalf("sent prompts = %#v", agentSession.sentPrompts)
	}
	if got := platform.getSent(); len(got) != 1 || got[0] != "done" {
		t.Fatalf("silent webhook must suppress only ingress status, not agent result: %#v", got)
	}
}

func TestWebhookServer_PromptQueuesWhenSessionBusy(t *testing.T) {
	platform := &webhookTestPlatform{stubPlatformEngine: stubPlatformEngine{n: "telegram"}}
	engine := NewEngine("fix-project", &stubAgent{}, []Platform{platform}, "", LangEnglish)
	const sessionKey = "telegram:1:1"
	session := engine.sessions.GetOrCreateActive(sessionKey)
	if !session.TryLock() {
		t.Fatal("failed to prepare busy session")
	}
	defer session.Unlock()
	engine.ensureInteractiveStateForQueueing(sessionKey, platform, sessionKey)

	ws := NewWebhookServer(0, "", "/hook")
	ws.RegisterEngine("fix-project", engine)
	recorder := postWebhookPrompt(t, ws, WebhookRequest{
		Project:    "fix-project",
		SessionKey: sessionKey,
		Prompt:     "dispatch-id=queued",
		Silent:     true,
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeWebhookResponse(t, recorder)
	if response["status"] != "accepted" || response["delivery"] != "queued" {
		t.Fatalf("response = %#v, want accepted/queued", response)
	}

	engine.interactiveMu.Lock()
	state := engine.interactiveStates[sessionKey]
	engine.interactiveMu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.pendingMessages) != 1 || state.pendingMessages[0].content != "dispatch-id=queued" {
		t.Fatalf("pending messages = %#v", state.pendingMessages)
	}
	if got := platform.getSent(); len(got) != 0 {
		t.Fatalf("silent queued webhook sent platform messages: %#v", got)
	}
}

func TestWebhookServer_PromptRejectsUnknownPlatformBeforeReturningAccepted(t *testing.T) {
	engine := NewEngine("fix-project", &stubAgent{}, nil, "", LangEnglish)
	ws := NewWebhookServer(0, "", "/hook")
	ws.RegisterEngine("fix-project", engine)

	recorder := postWebhookPrompt(t, ws, WebhookRequest{
		Project:    "fix-project",
		SessionKey: "telegram:1:1",
		Prompt:     "dispatch-id=rejected",
		Silent:     true,
	})

	if recorder.Code < 400 {
		t.Fatalf("status = %d, want non-2xx; body=%s", recorder.Code, recorder.Body.String())
	}
}
