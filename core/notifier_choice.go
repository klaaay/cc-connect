package core

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

func notifierCurrentField(s *notifierSelection) notifierField {
	f := s.task.Inputs[s.field]
	if excluded, ok := s.input[f.ExcludeField].(string); ok {
		options := make([]string, 0, len(f.Options))
		for _, option := range f.Options {
			if option != excluded {
				options = append(options, option)
			}
		}
		f.Options = options
	}
	return f
}
func parseNotifierMultiChoice(f notifierField, text string) ([]string, error) {
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '，' || unicode.IsSpace(r) })
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty selection")
	}
	selected := make([]string, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		index, err := strconv.Atoi(part)
		if err != nil || index < 1 || index > len(f.Options) || seen[index] {
			return nil, fmt.Errorf("invalid or repeated option")
		}
		seen[index] = true
		selected = append(selected, f.Options[index-1])
	}
	return selected, nil
}
func notifierDefaultInput(f notifierField) (any, error) {
	indices := []string{}
	values := []string{}
	switch v := f.DefaultValue.(type) {
	case bool:
		return parseNotifierInput(f, strconv.FormatBool(v))
	case float64:
		return parseNotifierInput(f, strconv.FormatFloat(v, 'f', -1, 64))
	case string:
		values = []string{v}
	case []string:
		values = v
	case []any:
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("invalid default")
			}
			values = append(values, text)
		}
	default:
		return nil, fmt.Errorf("unsupported default")
	}
	for _, value := range values {
		found := false
		for i, option := range f.Options {
			if option == value {
				indices = append(indices, strconv.Itoa(i+1))
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("default outside options")
		}
	}
	if f.Type == "text" && len(indices) != 1 {
		return nil, fmt.Errorf("invalid default")
	}
	return parseNotifierInput(f, strings.Join(indices, ","))
}
func (e *Engine) notifierTaskConfirmation(s *notifierSelection) string {
	text := s.task.Label
	if s.task.TrackingProject != "" {
		text += "\n" + e.i18n.Tf(MsgWNTrackingProject, s.task.TrackingProject)
	}
	for _, f := range s.task.Inputs {
		if value, ok := s.input[f.Key]; ok {
			text += fmt.Sprintf("\n%s: %v", f.Label, value)
		}
		if f.Key == "targetBranches" {
			text += "\n" + e.i18n.T(MsgWNSyncSteps)
		}
	}
	return text + "\n" + e.i18n.T(MsgWNConfirmTask)
}
func (e *Engine) notifierConfirmTask(p Platform, msg *Message, s *notifierSelection, text string) bool {
	if text != "执行" && !strings.EqualFold(text, "execute") {
		e.reply(p, msg.ReplyCtx, e.notifierTaskConfirmation(s))
		return true
	}
	s.stage = "submitted"
	e.submitNotifierTask(p, msg, s)
	return true
}
