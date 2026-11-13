package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceReady_RequiresExplicitAuthenticatedStatus(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent")
	for _, value := range []string{`{"loggedIn":true}`, `{"loggedIn":false}`, `{}`} {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' '"+value+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		a := &Agent{cmd: binary, workDir: dir, activeIdx: -1}
		if got := a.MaintenanceReady(context.Background()); (got == nil) != (value == `{"loggedIn":true}`) {
			t.Fatal("authentication result mismatch")
		}
	}
}
