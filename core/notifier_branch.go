package core

import (
	"regexp"
	"strings"
)

var notifierBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// 与 Notifier 的正式发布分支合同一致；服务端仍负责远端存在性和 SHA 固定。
func validNotifierBranch(branch string) bool {
	if len(branch) > 200 || !notifierBranchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.HasSuffix(branch, "/") {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func (e *Engine) notifierBranchInput(p Platform, msg *Message, s *notifierSelection, text string) bool {
	if !validNotifierBranch(text) {
		e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNInvalid))
		return true
	}
	s.branch = text
	s.stage = "submitted"
	e.submitNotifierDeployment(p, msg, s, s.target)
	return true
}
