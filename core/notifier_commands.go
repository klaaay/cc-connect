package core

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const notifierPageSize = 10
const notifierSelectionTTL = 5 * time.Minute

type notifierInteraction struct {
	trackingMu sync.Mutex
	tracking   map[string]bool
	client     *notifierClient
	mu         sync.Mutex
	selections map[string]*notifierSelection
}
type notifierSelection struct {
	expires     time.Time
	command     string
	stage       string
	page        int
	tasks       []notifierTask
	deployments []notifierDeployment
	runs        []notifierRun
	task        notifierTask
	deployment  notifierDeployment
	environment string
	input       map[string]any
	field       int
}

func (e *Engine) SetNotifier(cfg NotifierConfig) error {
	client, err := newNotifierClient(cfg)
	if err != nil {
		return err
	}
	e.notifier = &notifierInteraction{client: client, selections: make(map[string]*notifierSelection), tracking: make(map[string]bool)}
	return nil
}
func notifierSelectionKey(p Platform, msg *Message) string {
	return p.Name() + "\x00" + msg.SessionKey + "\x00" + msg.UserID
}
func (e *Engine) clearNotifierSelection(p Platform, msg *Message) {
	if e.notifier == nil {
		return
	}
	e.notifier.mu.Lock()
	defer e.notifier.mu.Unlock()
	delete(e.notifier.selections, notifierSelectionKey(p, msg))
}
func (e *Engine) cmdNotifier(p Platform, msg *Message, command string) {
	if e.notifier == nil {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNUnavailable))
		return
	}
	if msg.InputOrigin == "automation" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNHumanOnly))
		return
	}
	if command == "wn_help" {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNHelp))
		return
	}
	n := e.notifier
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, selection := range n.selections {
		if time.Now().After(selection.expires) {
			delete(n.selections, key)
		}
	}
	s := &notifierSelection{command: command, stage: command, expires: time.Now().Add(notifierSelectionTTL)}
	if command == "wn_runs" {
		runs, err := n.client.runs(e.ctx)
		if err != nil {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNQueryFailed))
			return
		}
		s.runs = runs
	} else {
		catalog, err := n.client.catalog(e.ctx)
		if err != nil {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNQueryFailed))
			return
		}
		s.tasks = catalog.Tasks
		s.deployments = catalog.Deployments
	}
	n.selections[notifierSelectionKey(p, msg)] = s
	e.reply(p, msg.ReplyCtx, e.notifierMenu(s))
}
func (e *Engine) notifierMenu(s *notifierSelection) string {
	var lines []string
	switch s.stage {
	case "wn_tasks":
		for _, task := range s.tasks {
			label := task.Label
			if !task.Enabled {
				label += " · " + e.i18n.T(MsgWNDisabled)
			}
			lines = append(lines, label)
		}
	case "wn_deploy":
		for _, d := range s.deployments {
			lines = append(lines, d.Label+" · "+d.Branch+" · "+d.TaskID)
		}
	case "wn_runs":
		for _, r := range s.runs {
			lines = append(lines, r.Label+" · "+e.notifierStatus(r.Status))
		}
	case "environment":
		lines = s.deployment.Environments
	case "target":
		lines = s.deployment.Targets
	}
	titleKey := s.stage
	if titleKey == "environment" || titleKey == "target" {
		titleKey = "wn_" + titleKey
	}
	title := e.i18n.T(MsgKey(titleKey))
	if len(lines) == 0 {
		return title + "\n" + e.i18n.T(MsgWNEmpty)
	}
	start := s.page * notifierPageSize
	end := min(start+notifierPageSize, len(lines))
	var b strings.Builder
	b.WriteString(title + "\n\n")
	for i := start; i < end; i++ {
		prefix := ""
		if s.stage == "wn_tasks" && s.tasks[i].Pinned {
			prefix = "📌 "
		}
		fmt.Fprintf(&b, "%s%d. %s\n", prefix, i+1, lines[i])
	}
	b.WriteString("\n" + e.i18n.Tf(MsgWNSelect, start+1, end, len(lines)))
	return b.String()
}
func (e *Engine) handleNotifierSelection(p Platform, msg *Message, content string) bool {
	n := e.notifier
	if n == nil || msg.InputOrigin == "automation" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	key := notifierSelectionKey(p, msg)
	s := n.selections[key]
	if s == nil {
		return false
	}
	if time.Now().After(s.expires) {
		delete(n.selections, key)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNExpired))
		return true
	}
	if !e.notifierSelectionAllowed(msg, s.command) {
		delete(n.selections, key)
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgAdminRequired, "/"+s.command))
		return true
	}
	text := strings.TrimSpace(content)
	if text == "取消" || strings.EqualFold(text, "cancel") {
		delete(n.selections, key)
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNCancelled))
		return true
	}
	if s.stage == "submitted" {
		if _, err := strconv.Atoi(text); err == nil {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNAlreadySubmitted))
			return true
		}
		delete(n.selections, key)
		return false
	}
	s.expires = time.Now().Add(notifierSelectionTTL)
	if s.stage == "input" {
		return e.notifierInput(p, msg, key, s, text)
	}
	count := len(s.tasks)
	switch s.stage {
	case "wn_deploy":
		count = len(s.deployments)
	case "wn_runs":
		count = len(s.runs)
	case "environment":
		count = len(s.deployment.Environments)
	case "target":
		count = len(s.deployment.Targets)
	}
	if text == "下一页" || text == "next" {
		if (s.page+1)*notifierPageSize < count {
			s.page++
		}
		e.reply(p, msg.ReplyCtx, e.notifierMenu(s))
		return true
	}
	if text == "上一页" || text == "prev" {
		if s.page > 0 {
			s.page--
		}
		e.reply(p, msg.ReplyCtx, e.notifierMenu(s))
		return true
	}
	index, err := strconv.Atoi(text)
	if err != nil || index < 1 || index > count || index <= s.page*notifierPageSize || index > (s.page+1)*notifierPageSize {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNInvalid))
		return true
	}
	index--
	switch s.stage {
	case "wn_tasks":
		s.task = s.tasks[index]
		if !s.task.Enabled {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNDisabled))
			return true
		}
		s.input = make(map[string]any)
		if len(s.task.Inputs) > 0 {
			s.stage = "input"
			e.reply(p, msg.ReplyCtx, e.notifierFieldPrompt(s))
			return true
		}
		s.stage = "submitted"
		e.submitNotifierTask(p, msg, s)
	case "wn_deploy":
		s.deployment = s.deployments[index]
		s.stage = "environment"
		s.page = 0
		e.reply(p, msg.ReplyCtx, e.notifierMenu(s))
	case "environment":
		s.environment = s.deployment.Environments[index]
		s.stage = "target"
		s.page = 0
		e.reply(p, msg.ReplyCtx, e.notifierMenu(s))
	case "target":
		s.stage = "submitted"
		e.submitNotifierDeployment(p, msg, s, s.deployment.Targets[index])
	case "wn_runs":
		r := s.runs[index]
		current, err := n.client.run(e.ctx, r.Kind, r.ID)
		if err != nil {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNQueryFailed))
		} else {
			e.reply(p, msg.ReplyCtx, e.notifierRunText(current))
		}
	}
	return true
}
func (e *Engine) notifierSelectionAllowed(msg *Message, command string) bool {
	if !e.isAdmin(msg.UserID) {
		return false
	}
	e.userRolesMu.RLock()
	defer e.userRolesMu.RUnlock()
	disabled := e.disabledCmds
	if e.userRoles != nil {
		if role := e.userRoles.ResolveRole(msg.UserID); role != nil {
			disabled = role.DisabledCmds
		}
	}
	return !disabled[command]
}
func notifierRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto random unavailable")
	}
	return fmt.Sprintf("%x", value)
}
func (e *Engine) notifierStatus(status string) string { return e.i18n.T(MsgKey("wn_status_" + status)) }
func (e *Engine) notifierRunText(run notifierRun) string {
	return fmt.Sprintf("%s\n%s\n%s\n%s", run.Label, run.ID, e.notifierStatus(run.Status), run.Message)
}
