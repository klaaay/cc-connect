package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type notifierTestServer struct {
	mu        sync.Mutex
	writes    []map[string]any
	paths     []string
	catalog   notifierCatalog
	failWrite bool
}

func newNotifierTestServer(t *testing.T) (*notifierTestServer, NotifierConfig) {
	t.Helper()
	state := &notifierTestServer{catalog: notifierCatalog{Version: 1, Tasks: []notifierTask{
		{ID: "pinned", Label: "Pinned task", Revision: "v1", Pinned: true, Enabled: true},
		{ID: "ordinary", Label: "Ordinary task", Revision: "v1", Enabled: true, Inputs: []notifierField{{Key: "issue", Label: "Issue", Type: "number", Required: true}}},
	}, Deployments: []notifierDeployment{{TaskID: "release", Label: "team/repo", Branch: "main", Environments: []string{"qa", "prod"}, Targets: []string{"web", "hub"}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "auth", Value: "yes", Path: "/"})
			json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
			return
		}
		if cookie, err := r.Cookie("auth"); err != nil || cookie.Value != "yes" {
			w.WriteHeader(401)
			return
		}
		var result any
		switch {
		case r.URL.Path == "/wn/catalog":
			result = state.catalog
		case r.URL.Path == "/automations/merge-task/candidate":
			result = map[string]string{"revision": strings.Repeat("a", 40), "taskId": "release", "branch": "main"}
		case r.Method == "POST":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			state.writes = append(state.writes, body)
			state.paths = append(state.paths, r.URL.Path)
			if state.failWrite {
				w.WriteHeader(503)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/task-instances/") {
				result = notifierRun{ID: "run-1", TaskID: strings.Split(r.URL.Path, "/")[2], Status: "queued"}
			} else {
				result = map[string]bool{"accepted": true}
			}
		case r.URL.Path == "/wn/runs":
			result = map[string]any{"items": []notifierRun{{Kind: "task", ID: "run-1", TaskID: "pinned", Label: "Pinned task", Status: "succeeded"}}}
		default:
			result = notifierRun{Kind: "task", ID: "run-1", TaskID: "pinned", Label: "Pinned task", Status: "succeeded"}
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": result})
	}))
	t.Cleanup(server.Close)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	return state, NotifierConfig{BaseURL: server.URL, TokenFile: tokenFile}
}
func TestNotifierClientDoesNotRetryWrites(t *testing.T) {
	state, cfg := newNotifierTestServer(t)
	state.failWrite = true
	client, err := newNotifierClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var result notifierRun
	if err = client.call(context.Background(), "/task-instances/pinned/run", "run-task", map[string]string{"requestId": "once"}, &result); err == nil {
		t.Fatal("expected error")
	}
	if len(state.writes) != 1 {
		t.Fatal("write retried")
	}
}
func TestNotifierClientRejectsRemoteHTTP(t *testing.T) {
	if _, err := newNotifierClient(NotifierConfig{BaseURL: "http://example.com", TokenFile: "token"}); err == nil {
		t.Fatal("remote HTTP accepted")
	}
}
