package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceReady_AuthenticationFailureIsClosed(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent")
	for _, code := range []string{"0", "1"} {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit "+code+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		a := &Agent{cmd: binary, workDir: dir, activeIdx: -1}
		if got := a.MaintenanceReady(context.Background()); (got == nil) != (code == "0") {
			t.Fatal("authentication result mismatch")
		}
	}
}
