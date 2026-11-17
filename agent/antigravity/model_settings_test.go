package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModelSettings_CatalogGroupsCLIModelsAndEfforts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "agy")
	script := "#!/bin/sh\n[ \"$GEMINI_API_KEY\" = configured ] || exit 1\nprintf 'Fetching available models...\\ngemini-4-carbon-high\\tGemini 4 Carbon (High)\\ngemini-4-carbon-low\\tGemini 4 Carbon (Low)\\ngemini-4-hydrogen-xhigh\\tGemini 4 Hydrogen (XHigh)\\ngemini-4-argon-medium\\tGemini 4 Argon (Medium)\\n'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a := &Agent{cmd: binary, workDir: t.TempDir(), configEnv: []string{"GEMINI_API_KEY=configured"}, activeIdx: -1}
	models := a.AvailableModels(context.Background())
	if len(models) != 3 || models[0].Name != "gemini-4-carbon" {
		t.Fatalf("models = %+v", models)
	}
	efforts := a.ModelReasoningEfforts(context.Background())
	if !reflect.DeepEqual(efforts["gemini-4-carbon"], []string{"low", "high"}) {
		t.Fatalf("efforts=%v", efforts)
	}
	a.SetModel("gemini-4-hydrogen")
	if !reflect.DeepEqual(a.AvailableReasoningEfforts(), []string{"xhigh"}) {
		t.Fatal(a.AvailableReasoningEfforts())
	}
}

func TestModelSettings_StartSessionPassesModelAndEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, model := range []string{"gemini-4-hydrogen", "gemini-4-carbon", "gemini-4-argon"} {
		raw, err := New(map[string]any{"cmd": "echo", "mode": "yolo", "model": model, "reasoning_effort": "medium"})
		if err != nil {
			t.Fatal(err)
		}
		a := raw.(*Agent)
		session, err := a.StartSession(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		s := session.(*antigravitySession)
		args := s.buildAntigravityArgs("", false, "yolo", "", "hello")
		if !contains(args, "--model") || !contains(args, model) || !contains(args, "--effort") || !contains(args, "medium") {
			t.Fatalf("args=%v", args)
		}
		_ = session.Close()
	}
}

func TestMaintenanceReady_FailsClosedWithoutLeakingCLIOutput(t *testing.T) {
	for _, script := range []string{"#!/bin/sh\necho SECRET >&2\nexit 1\n", "#!/bin/sh\necho 'Welcome! Please sign in'\n"} {
		path := filepath.Join(t.TempDir(), "agy")
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		a := &Agent{cmd: path, workDir: t.TempDir(), activeIdx: -1}
		err := a.MaintenanceReady(context.Background())
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("error=%v", err)
		}
	}
}
