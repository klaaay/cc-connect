package antigravity

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/chenhg5/cc-connect/core"
)

type agyToolProgress struct {
	started bool
	done    bool
	name    string
}

var agyCommandExit = regexp.MustCompile(`(?m)^Command exited with code (-?\d+)\.$`)

// 同一个步骤会多次更新；只转发一次开始和结束，不把状态快照重复发到聊天中。
func (as *antigravitySession) emitToolProgress(event agyStreamEvent, steps map[string]*agyToolProgress) {
	step := event.Step
	if step.Index == nil || (step.State != "ACTIVE" && step.State != "DONE" && step.State != "ERROR") {
		return
	}
	sid := step.ConversationID
	if sid == "" {
		sid = as.CurrentSessionID()
	}
	key := fmt.Sprintf("%s:%d", sid, *step.Index)
	progress := steps[key]
	if progress == nil {
		progress = &agyToolProgress{}
		steps[key] = progress
	}
	if progress.done {
		return
	}
	name := step.ToolName
	if name == "" {
		name = step.ToolInfo.Name
	}
	if name != "" {
		progress.name = name
	}
	if progress.name == "" {
		return
	}
	if !progress.started {
		input := ""
		if len(step.ToolInfo.Parameters) > 0 {
			data, _ := json.Marshal(step.ToolInfo.Parameters) // 来自已解码的 JSON，均为可序列化值。
			input = string(data)
		}
		as.emit(core.Event{Type: core.EventToolUse, ToolName: progress.name, ToolInput: input, ToolInputRaw: step.ToolInfo.Parameters})
		progress.started = true
	}
	if step.State == "DONE" || step.State == "ERROR" {
		as.emit(agyToolResult(event, progress.name))
		progress.done = true
	}
}

func agyToolResult(event agyStreamEvent, name string) core.Event {
	step := event.Step
	// DONE 只代表工具已结束；输出可能是错误，不据此推断执行成功。
	result := core.Event{Type: core.EventToolResult, ToolName: name, ToolResult: formatAgyOutputValue(step.ToolInfo.Output), ToolStatus: "completed"}
	if name == "run_command" {
		if match := agyCommandExit.FindStringSubmatch(result.ToolResult); len(match) > 1 {
			if code, err := strconv.Atoi(match[1]); err == nil {
				result.ToolExitCode = &code
				success := code == 0
				result.ToolSuccess = &success
				if !success {
					result.ToolStatus = "failed"
				}
			}
		}
	}
	if step.State == "ERROR" || step.ToolInfo.Error != nil {
		result.ToolStatus = "failed"
		success := false
		result.ToolSuccess = &success
		if step.ToolInfo.Error != nil {
			if result.ToolResult != "" {
				result.ToolResult += "\n"
			}
			result.ToolResult += step.ToolInfo.Error.Message
		}
	}
	return result
}
