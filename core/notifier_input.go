package core

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var notifierGitlabUsername = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,254}$`)

func (e *Engine) notifierFieldPrompt(s *notifierSelection) string {
	f := notifierCurrentField(s)
	text := e.i18n.Tf(MsgWNField, f.Label, f.Type)
	if f.Options != nil {
		for i, option := range f.Options {
			text += fmt.Sprintf("\n%d. %s", i+1, option)
		}
	}
	if f.Type == "lines" && f.Options != nil {
		text += "\n" + e.i18n.T(MsgWNMultiChoice)
	}
	if f.DefaultValue != nil {
		text += "\n" + e.i18n.Tf(MsgWNDefaultChoice, fmt.Sprint(f.DefaultValue))
	}
	if f.Type == "boolean" {
		text += "\n1. true\n2. false"
	}
	if f.Min != nil {
		text += fmt.Sprintf("\nmin: %g", *f.Min)
	}
	if f.Max != nil {
		text += fmt.Sprintf("\nmax: %g", *f.Max)
	}
	if !f.Required && f.Type == "text" && f.Options == nil {
		text += "\n" + e.i18n.T(MsgWNClear)
	}
	if !f.Required {
		text += "\n" + e.i18n.T(MsgWNSkip)
	}
	return text
}
func parseNotifierInput(f notifierField, text string) (any, error) {
	text = strings.TrimSpace(text)
	if (text == "默认" || strings.EqualFold(text, "default")) && f.DefaultValue != nil {
		return notifierDefaultInput(f)
	}
	if !f.Required && f.Type == "text" && f.Options == nil && (text == "清空" || strings.EqualFold(text, "clear")) {
		return "", nil
	}
	return parseNotifierValue(f, text)
}

// 默认值按字面值验证，避免把 default 等合法文本再次解释成操作命令。
func parseNotifierValue(f notifierField, text string) (any, error) {
	if f.Type == "lines" && f.Options != nil {
		return parseNotifierMultiChoice(f, text)
	}

	if f.Options != nil {
		index, err := strconv.Atoi(text)
		if err != nil || index < 1 || index > len(f.Options) {
			return nil, fmt.Errorf("invalid option")
		}
		text = f.Options[index-1]
	}
	if f.MaxLength > 0 && utf8.RuneCountInString(text) > f.MaxLength {
		return nil, fmt.Errorf("too long")
	}
	switch f.Type {
	case "text":
		if f.Required && text == "" {
			return nil, fmt.Errorf("required")
		}
		if text != "" && f.Format == "gitlab-username" && !notifierGitlabUsername.MatchString(text) {
			return nil, fmt.Errorf("invalid gitlab username")
		}
		return text, nil
	case "number":
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || (f.Min != nil && value < *f.Min) || (f.Max != nil && value > *f.Max) {
			return nil, fmt.Errorf("invalid number")
		}
		return value, nil
	case "boolean":
		if text == "1" || text == "true" {
			return true, nil
		}
		if text == "2" || text == "false" {
			return false, nil
		}
		return nil, fmt.Errorf("invalid boolean")
	case "lines":
		return strings.Split(text, "\n"), nil
	default:
		return nil, fmt.Errorf("unsupported input type")
	}
}
func (e *Engine) notifierInput(p Platform, msg *Message, key string, s *notifierSelection, text string) bool {
	f := notifierCurrentField(s)
	if (text != "跳过" && !strings.EqualFold(text, "skip")) || f.Required {
		value, err := parseNotifierInput(f, text)
		if err != nil {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWNInvalid)+"\n"+e.notifierFieldPrompt(s))
			return true
		}
		s.input[f.Key] = value
	}
	s.field++
	if s.field < len(s.task.Inputs) {
		e.reply(p, msg.ReplyCtx, e.notifierFieldPrompt(s))
		return true
	}
	s.stage = "confirm-task"
	e.reply(p, msg.ReplyCtx, e.notifierTaskConfirmation(s))
	return true
}
