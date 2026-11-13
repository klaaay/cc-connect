package codex

import (
	"context"
	"errors"
	"github.com/chenhg5/cc-connect/core"
	"os"
	"os/exec"
	"time"
)

func (a *Agent) MaintenanceReady(ctx context.Context) error {
	a.mu.Lock()
	binary, dir, home := a.cmd, a.workDir, a.codexHome
	env := core.MergeEnv(os.Environ(), append(append([]string{}, a.configEnv...), a.providerEnvLocked()...))
	a.mu.Unlock()
	if home != "" {
		env = core.MergeEnv(env, []string{"CODEX_HOME=" + home})
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "login", "status")
	cmd.Dir, cmd.Env = dir, env
	// Output is deliberately discarded: login status may include account details.
	if err := cmd.Run(); err != nil {
		return errors.New("agent authentication unavailable")
	}
	return nil
}
