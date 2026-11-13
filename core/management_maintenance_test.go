package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenance_ManagementRestartIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := OpenMaintenance(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	InstallMaintenance(g)
	defer InstallMaintenance(nil)
	m := NewManagementServer(0, "token", nil)
	m.SetConfigFilePath(path)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/maintenance", m.wrap(m.handleMaintenance))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	rev := configurationRevision(path)
	result := mgmtPost(t, ts.URL+"/api/v1/maintenance", "token", map[string]string{"action": "begin", "operation_id": "op", "revision": rev})
	if !result.OK {
		t.Fatal(result.Error)
	}
	if err := os.WriteFile(path, []byte("b"), 0600); err != nil {
		t.Fatal(err)
	}
	result = mgmtPost(t, ts.URL+"/api/v1/maintenance", "token", map[string]string{"action": "end", "operation_id": "op", "revision": configurationRevision(path)})
	if result.OK {
		t.Fatal("disk was mistaken for runtime")
	}
	fresh, err := OpenMaintenance(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	InstallMaintenance(fresh)
	m.SetConfigFilePath(path)
	result = mgmtPost(t, ts.URL+"/api/v1/maintenance", "token", map[string]string{"action": "end", "operation_id": "op", "revision": configurationRevision(path)})
	if !result.OK {
		t.Fatal(result.Error)
	}
}
