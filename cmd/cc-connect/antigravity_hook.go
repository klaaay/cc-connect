package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/chenhg5/cc-connect/agent/antigravityhook"
)

func runAntigravityPermissionHook() {
	event := ""
	if len(os.Args) > 2 {
		event = os.Args[2]
	}
	err := antigravityhook.Relay(
		os.Stdin,
		os.Stdout,
		os.Getenv(antigravityhook.EnvAddress),
		os.Getenv(antigravityhook.EnvToken),
		event,
	)
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "cc-connect Agy permission hook: %v\n", err)
	_ = json.NewEncoder(os.Stdout).Encode(antigravityhook.BridgeResponse{
		Decision: "deny",
		Reason:   "cc-connect permission bridge is unavailable",
	})
}
