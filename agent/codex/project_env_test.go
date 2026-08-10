package codex

import (
	"reflect"
	"testing"
)

// TestNew_ParsesProjectEnvFromOpts verifies that env vars declared under
// [projects.agent.options.env] in config.toml are loaded into the agent's
// configEnv field. Without this, user-scoped env (e.g. HTTPS_PROXY in the
// shell that launched cc-connect) silently overrides the values intended
// for the codex subprocess.
//
// Regression for: codex agent ignoring opts["env"] in factory.
func TestNew_ParsesProjectEnvFromOpts(t *testing.T) {
	// Use "go" as cliBin to satisfy exec.LookPath without requiring codex
	// to be installed on the test runner.
	opts := map[string]any{
		"work_dir": t.TempDir(),
		"cmd":      "go",
		"env": map[string]string{
			"HTTPS_PROXY": "http://127.0.0.1:10808",
			"HTTP_PROXY":  "http://127.0.0.1:10808",
			"ALL_PROXY":   "http://127.0.0.1:10808",
		},
	}

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	agent := a.(*Agent)
	agent.mu.RLock()
	got := envSliceToMap(agent.configEnv)
	agent.mu.RUnlock()

	if len(got) != 3 {
		t.Fatalf("expected 3 env vars, got %d: %v", len(got), agent.configEnv)
	}
	if v := got["HTTPS_PROXY"]; v != "http://127.0.0.1:10808" {
		t.Errorf("HTTPS_PROXY = %q, want http://127.0.0.1:10808", v)
	}
	if v := got["ALL_PROXY"]; v != "http://127.0.0.1:10808" {
		t.Errorf("ALL_PROXY = %q, want http://127.0.0.1:10808", v)
	}
}

// TestNew_ParsesProjectEnvFromMapStringAny covers the TOML decoder path
// where the env table arrives as map[string]any rather than map[string]string.
func TestNew_ParsesProjectEnvFromMapStringAny(t *testing.T) {
	opts := map[string]any{
		"work_dir": t.TempDir(),
		"cmd":      "go",
		"env": map[string]any{
			"OPENAI_BASE_URL": "https://api.example.com/v1",
			"CUSTOM_FLAG":     "yes",
		},
	}

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	agent := a.(*Agent)
	agent.mu.RLock()
	got := envSliceToMap(agent.configEnv)
	agent.mu.RUnlock()

	if v := got["OPENAI_BASE_URL"]; v != "https://api.example.com/v1" {
		t.Errorf("OPENAI_BASE_URL = %q", v)
	}
	if v := got["CUSTOM_FLAG"]; v != "yes" {
		t.Errorf("CUSTOM_FLAG = %q", v)
	}
}

// TestNew_NoEnvOpts ensures the absence of an env block produces an empty
// configEnv slice (no panics, no surprise inheritance).
func TestNew_NoEnvOpts(t *testing.T) {
	opts := map[string]any{
		"work_dir": t.TempDir(),
		"cmd":      "go",
	}

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	agent := a.(*Agent)
	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if len(agent.configEnv) != 0 {
		t.Fatalf("expected 0 env vars, got %d: %v", len(agent.configEnv), agent.configEnv)
	}
}

func TestNew_ParsesProjectDisabledSkills(t *testing.T) {
	tests := []struct {
		name string
		raw  any
	}{
		{name: "toml array", raw: []any{"superpowers-using-superpowers", "popo-im"}},
		{name: "string slice", raw: []string{"superpowers-using-superpowers", "popo-im"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := New(map[string]any{
				"work_dir":        t.TempDir(),
				"cmd":             "go",
				"backend":         "app_server",
				"disabled_skills": tt.raw,
			})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			got := a.(*Agent).DisabledSkillNames()
			want := []string{"superpowers-using-superpowers", "popo-im"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("DisabledSkillNames() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestNew_WithoutDisabledSkillsKeepsProjectUnfiltered(t *testing.T) {
	a, err := New(map[string]any{"work_dir": t.TempDir(), "cmd": "go", "backend": "app_server"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if got := a.(*Agent).DisabledSkillNames(); len(got) != 0 {
		t.Fatalf("DisabledSkillNames() = %#v, want empty", got)
	}
}

func TestNew_RejectsInvalidDisabledSkills(t *testing.T) {
	_, err := New(map[string]any{
		"work_dir":        t.TempDir(),
		"cmd":             "go",
		"disabled_skills": []any{"valid", 42},
	})
	if err == nil {
		t.Fatal("New() must reject non-string disabled_skills entries")
	}
}

func TestNew_ParsesProjectPromptsFromOpts(t *testing.T) {
	opts := map[string]any{
		"work_dir":             t.TempDir(),
		"cli_path":             "go",
		"system_prompt":        "You are Linear Reporter.",
		"append_system_prompt": "Always use linear-bug-intake.",
	}

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	agent := a.(*Agent)
	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if agent.systemPrompt != "You are Linear Reporter." {
		t.Fatalf("systemPrompt = %q", agent.systemPrompt)
	}
	if agent.appendPrompt != "Always use linear-bug-intake." {
		t.Fatalf("appendPrompt = %q", agent.appendPrompt)
	}
}
