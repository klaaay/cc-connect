package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
)

var ErrMaintenance = errors.New("execution paused for configuration maintenance")
var processMaintenance atomic.Pointer[Maintenance]
var maintenanceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type MaintenanceState struct {
	OperationID      string            `json:"operation_id"`
	Revision         string            `json:"revision"`
	Phase            string            `json:"phase"`
	RestartID        string            `json:"restart_id,omitempty"`
	InstanceID       string            `json:"instance_id"`
	Active           int               `json:"active"`
	Completed        map[string]string `json:"completed,omitempty"`
	RestartRevisions map[string]string `json:"restart_revisions,omitempty"`
	Restarts         []string          `json:"restarts,omitempty"`
}

// Maintenance is process-wide: every execution ticket and maintenance admission
// uses this mutex. It is installed before engines and input listeners start.
type Maintenance struct {
	mu     sync.Mutex
	path   string
	state  MaintenanceState
	active int
}

func OpenMaintenance(path string) (*Maintenance, error) {
	g := &Maintenance{path: path}
	data, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(data, &g.state) != nil || (g.state.Phase != "paused" && g.state.Phase != "restarting" && g.state.Phase != "released") || (g.state.Phase == "released" && g.state.OperationID != "") || (g.state.Phase != "released" && (!maintenanceID.MatchString(g.state.OperationID) || g.state.Revision == "")) {
			return nil, errors.New("invalid maintenance state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("maintenance state unavailable")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	g.state.InstanceID = hex.EncodeToString(id[:])
	return g, nil
}

func InstallMaintenance(g *Maintenance) { processMaintenance.Store(g) }
func enterExecution() (func(), error) {
	if g := processMaintenance.Load(); g != nil {
		return g.Enter()
	}
	return func() {}, nil
}
func (g *Maintenance) Enter() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state.OperationID != "" {
		return nil, ErrMaintenance
	}
	g.active++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.active--; g.mu.Unlock() }) }, nil
}
func (g *Maintenance) Snapshot() MaintenanceState {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.state
	s.Active = g.active
	return s
}
func (g *Maintenance) persist(next MaintenanceState) error {
	if err := os.MkdirAll(filepath.Dir(g.path), 0700); err != nil {
		return errors.New("cannot persist maintenance state")
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(g.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot persist maintenance state")
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(g.path+".tmp", g.path)
	}
	if err != nil {
		return errors.New("cannot persist maintenance state")
	}
	g.state = next
	return nil
}
func (g *Maintenance) Begin(id, revision string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !maintenanceID.MatchString(id) || revision == "" {
		return errors.New("invalid maintenance request")
	}
	if _, done := g.state.Completed[id]; done {
		return errors.New("maintenance operation already completed")
	}
	if g.state.OperationID != "" {
		if g.state.OperationID == id && g.state.Revision == revision {
			return nil
		}
		return errors.New("another maintenance operation owns admission")
	}
	if g.active != 0 {
		return errors.New("executions are active")
	}
	return g.persist(MaintenanceState{OperationID: id, Revision: revision, Phase: "paused", InstanceID: g.state.InstanceID, Completed: g.state.Completed})
}
func (g *Maintenance) End(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, done := g.state.Completed[id]; g.state.OperationID == "" && done {
		return nil
	}
	if g.state.OperationID != id || id == "" {
		return errors.New("maintenance owner mismatch")
	}
	// Keep the completed operation ID for response-loss retries.
	completed := make(map[string]string)
	for k, v := range g.state.Completed {
		completed[k] = v
	}
	completed[id] = g.state.Revision
	return g.persist(MaintenanceState{Revision: id, Phase: "released", InstanceID: g.state.InstanceID, Completed: completed})
}
func (g *Maintenance) Restart(id, attempt string, revisions ...string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state.OperationID != id || id == "" || !maintenanceID.MatchString(attempt) {
		return false, errors.New("maintenance owner mismatch")
	}
	revision := ""
	if len(revisions) > 0 {
		revision = revisions[0]
	}
	for _, prior := range g.state.Restarts {
		if prior == attempt {
			if g.state.RestartRevisions[attempt] != revision {
				return false, errors.New("restart request changed")
			}
			return false, nil
		}
	}
	next := g.state
	next.RestartID = attempt
	next.RestartRevisions = make(map[string]string)
	for k, v := range g.state.RestartRevisions {
		next.RestartRevisions[k] = v
	}
	next.RestartRevisions[attempt] = revision
	next.Restarts = append(append([]string{}, g.state.Restarts...), attempt)
	next.Phase = "restarting"
	if err := g.persist(next); err != nil {
		return false, err
	}
	return true, nil
}
