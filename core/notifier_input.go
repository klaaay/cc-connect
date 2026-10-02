package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (e *Engine) notifierFieldPrompt(s *notifierSelection) string {
	f := s.task.Inputs[s.field]
	text := e.i18n.Tf(MsgWNField, f.Label, f.Type)
	if len(f.Options) > 0 {
		for i, option := range f.Options {
			text += fmt.Sprintf("\n%d. %s", i+1, option)
		}
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
	if !f.Required {
		text += "\n" + e.i18n.T(MsgWNSkip)
	}
	return text
}
func parseNotifierInput(f notifierField, text string) (any, error) {
	if len(f.Options) > 0 {
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
	f := s.task.Inputs[s.field]
	if (text != "跳过" && text != "skip") || f.Required {
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
	s.stage = "submitted"
	e.submitNotifierTask(p, msg, s)
	return true
}
