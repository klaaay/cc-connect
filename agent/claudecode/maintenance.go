package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/chenhg5/cc-connect/core"
	"os"
	"os/exec"
	"time"
)

func (a *Agent) MaintenanceReady(ctx context.Context) error {
	a.mu.Lock()
	binary, dir := a.cmd, a.workDir
	env := core.MergeEnv(os.Environ(), a.runtimeEnvLocked())
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "auth", "status", "--json")
	cmd.Dir, cmd.Env = dir, env
	data, err := cmd.Output()
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err != nil || json.Unmarshal(data, &status) != nil || !status.LoggedIn {
		return errors.New("agent authentication unavailable")
	}
	return nil
}
