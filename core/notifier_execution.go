package core

import (
	"encoding/hex"
	"net/url"
)

func (e *Engine) submitNotifierTask(p Platform, msg *Message, s *notifierSelection) {
	requestID := notifierRequestID()
	var run notifierRun
	err := e.notifier.client.call(e.ctx, "/task-instances/"+url.PathEscape(s.task.ID)+"/run", "run-task", map[string]any{
		"requestId": requestID, "revision": s.task.Revision, "input": s.input,
	}, &run)
	if err != nil || run.ID == "" || run.TaskID != s.task.ID {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWNSubmitUnknown, requestID))
		return
	}
	run.Kind = "task"
	run.Label = s.task.Label
	e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWNAccepted, run.Label, run.ID))
	e.watchNotifierRun(p, msg, run)
}
func (e *Engine) submitNotifierDeployment(p Platform, msg *Message, s *notifierSelection, target string) {
	var candidate struct {
		Revision string `json:"revision"`
		TaskID   string `json:"taskId"`
		Branch   string `json:"branch"`
	}
	err := e.notifier.client.call(e.ctx, "/automations/merge-task/candidate?"+url.Values{"taskId": []string{s.deployment.TaskID}}.Encode(), "", nil, &candidate)
	decoded, decodeErr := hex.DecodeString(candidate.Revision)
	if err != nil || decodeErr != nil || len(decoded) != 20 || candidate.TaskID != s.deployment.TaskID || candidate.Branch != s.deployment.Branch {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNQueryFailed))
		return
	}
	requestID := notifierRequestID()
	var result struct {
		Accepted bool `json:"accepted"`
	}
	err = e.notifier.client.call(e.ctx, "/automations/merge-task/deploy", "deploy-release", map[string]string{
		"requestId": requestID, "taskId": s.deployment.TaskID, "environment": s.environment, "target": target, "candidateSha": candidate.Revision,
	}, &result)
	if err != nil || !result.Accepted {
		e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWNSubmitUnknown, requestID))
		return
	}
	run := notifierRun{Kind: "deployment", ID: requestID, TaskID: s.deployment.TaskID, Label: s.deployment.Label + " · " + s.environment + " · " + target, Status: "queued"}
	e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWNAccepted, run.Label+" · "+candidate.Revision, run.ID))
	e.watchNotifierRun(p, msg, run)
}
