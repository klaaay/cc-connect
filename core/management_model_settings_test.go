package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMgmt_ProjectRuntimeModelSettings(t *testing.T) {
	agent := &stubModelModeAgent{model: "model-a", reasoningEffort: "medium"}
	e := NewEngine("test-project", agent, nil, "", LangEnglish)
	mgmt := NewManagementServer(0, "tok", nil)
	mgmt.RegisterEngine("test-project", e)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects/", mgmt.wrap(mgmt.handleProjectRoutes))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	for _, effort := range []string{"medium", "high"} {
		agent.SetReasoningEffort(effort)
		agent.SetModel("model-" + effort)
		result := mgmtGet(t, ts.URL+"/api/v1/projects/test-project", "tok")
		var data map[string]any
		if err := json.Unmarshal(result.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data["model"] != "model-"+effort || data["reasoning_effort"] != effort {
			t.Fatalf("not runtime settings: %v", data)
		}
	}
}
