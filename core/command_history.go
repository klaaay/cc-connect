package core

import "strings"

// Record consumed controls without arguments, which can contain credentials.
func (e *Engine) recordControlInput(p Platform, msg *Message, command string, args ...string) {
	if msg.InputOrigin == "automation" || !changesExecutionContext(command, args) {
		return
	}
	_, sessions, _, _, err := e.commandContextWithWorkspace(p, msg)
	if err != nil {
		return
	}
	recordSessionControl(sessions, msg.SessionKey, msg.InputOrigin, command)
}

func recordSessionControl(sessions *SessionManager, key, origin, command string) {
	session := sessions.FindByID(sessions.ActiveSessionID(key))
	if session == nil {
		return
	}
	session.AddHistoryWithOrigin("user", "CC_CONNECT_CONTROL /"+command, origin)
	sessions.Save()
}

func changesExecutionContext(command string, args []string) bool {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch command {
	case "help", "history", "status", "current", "list", "whoami", "ps", "show", "diff", "version", "usage", "skills", "doctor", "search":
		return false
	case "model", "mode", "reasoning", "lang", "name", "dir", "tts", "allow", "switch":
		return len(args) > 0
	case "provider":
		action := matchSubCommand(sub, []string{"list", "add", "remove", "switch", "current", "clear", "reset", "none"})
		return sub != "" && action != "list" && action != "current"
	case "config":
		return matchSubCommand(sub, []string{"get", "set", "reload"}) == "set" || matchSubCommand(sub, []string{"get", "set", "reload"}) == "reload"
	case "commands", "alias", "cron", "timer":
		return sub != "" && matchSubCommand(sub, []string{"list", "add", "addexec", "del", "delete", "rm", "remove", "exec", "run", "trigger", "enable", "disable", "mute", "unmute", "setup"}) != "list"
	case "heartbeat":
		return sub != "" && matchSubCommand(sub, []string{"status", "pause", "stop", "resume", "start", "run", "trigger", "interval"}) != "status"
	case "memory":
		if sub == "" {
			return false
		}
		action := matchSubCommand(sub, []string{"add", "global", "show", "help"})
		return action == "add" || (action == "global" && len(args) > 1 && matchSubCommand(strings.ToLower(args[1]), []string{"add", "show"}) == "add")
	default:
		return true
	}
}

// Caller holds managedInputMu so attribution and session changes share admission.
func (e *Engine) recordLegacyCardInput(command, args, sessionKey string) {
	command = strings.TrimPrefix(command, "/")
	if !changesExecutionContext(command, strings.Fields(args)) {
		return
	}
	_, sessions := e.sessionContextForKey(sessionKey)
	// The legacy callback has no trusted sender provenance.
	recordSessionControl(sessions, sessionKey, "", command)
}
