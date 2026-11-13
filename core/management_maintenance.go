package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
)

func configurationRevision(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (m *ManagementServer) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	g := processMaintenance.Load()
	if g == nil || m.token == "" || m.loadedRevision == "" {
		mgmtError(w, 503, "maintenance unavailable")
		return
	}
	if r.Method == http.MethodGet {
		m.maintenanceView(w, g)
		return
	}
	if r.Method != http.MethodPost {
		mgmtError(w, 405, "POST or GET only")
		return
	}
	var req struct {
		Action      string   `json:"action"`
		Projects    []string `json:"projects,omitempty"`
		OperationID string   `json:"operation_id"`
		Revision    string   `json:"revision"`
		RestartID   string   `json:"restart_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		mgmtError(w, 400, "invalid maintenance request")
		return
	}
	var err error
	switch req.Action {
	case "preflight":
		err = m.maintenancePreflight(r.Context(), g, req.OperationID, req.Projects)
	case "begin":
		if req.Revision != m.loadedRevision || configurationRevision(m.configFilePath) != req.Revision {
			err = errors.New("configuration changed")
		} else {
			err = g.Begin(req.OperationID, req.Revision)
		}
	case "restart":
		if req.Revision == "" || configurationRevision(m.configFilePath) != req.Revision {
			err = errors.New("saved configuration mismatch")
			break
		}
		var first bool
		first, err = g.Restart(req.OperationID, req.RestartID, req.Revision)
		if err == nil && first {
			select {
			case RestartCh <- RestartRequest{}:
			default:
				err = errors.New("restart not confirmed; inspect operation before recovery")
			}
		}
	case "end":
		if req.Revision == "" || req.Revision != m.loadedRevision || configurationRevision(m.configFilePath) != req.Revision {
			err = errors.New("runtime configuration not verified")
		} else {
			err = g.End(req.OperationID)
		}
	default:
		err = errors.New("unknown maintenance action")
	}
	if err != nil {
		mgmtError(w, 409, err.Error())
		return
	}
	m.maintenanceView(w, g)
}
func (m *ManagementServer) maintenanceView(w http.ResponseWriter, g *Maintenance) {
	rows := []map[string]any{}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, e := range m.engines {
		row := map[string]any{"project": name, "agent_type": e.agent.Name()}
		if v, ok := e.agent.(interface{ GetModel() string }); ok {
			row["model"] = v.GetModel()
		}
		if v, ok := e.agent.(interface{ GetReasoningEffort() string }); ok {
			row["reasoning_effort"] = v.GetReasoningEffort()
		}
		if v, ok := e.agent.(ReasoningEffortSwitcher); ok {
			row["efforts"] = v.AvailableReasoningEfforts()
		}
		rows = append(rows, row)
	}
	mgmtJSON(w, 200, map[string]any{"capability_version": 1, "maintenance": g.Snapshot(), "loaded_revision": m.loadedRevision, "disk_revision": configurationRevision(m.configFilePath), "projects": rows})
}

// Only the exact authenticated stop endpoint bypasses execution admission.
func maintenanceStopRoute(r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return r.Method == http.MethodPost && len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "projects" && parts[4] == "sessions" && parts[5] == "stop"
}
