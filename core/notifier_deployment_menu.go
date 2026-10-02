package core

import "strings"

type notifierDeploymentChoice struct {
	notifierDeployment
	environment string
	target      string
	context     string
}

// 按目录顺序展开环境和应用，序号快照同时固定工作区与部署参数。
func notifierDeploymentChoices(deployments []notifierDeployment) []notifierDeploymentChoice {
	var choices []notifierDeploymentChoice
	for _, d := range deployments {
		for _, environment := range d.Environments {
			for _, target := range d.Targets {
				choice := notifierDeploymentChoice{notifierDeployment: d, environment: environment, target: target}
				if len(deployments) > 1 {
					choice.context = d.Label + " · " + d.Branch + " · " + d.TaskID
				}
				choices = append(choices, choice)
			}
		}
	}
	return choices
}

func (e *Engine) notifierDeploymentLabel(choice notifierDeploymentChoice) string {
	target := choice.target
	switch target {
	case "web":
		target = "Web"
	case "hub":
		target = "Web Hub"
	}
	label := e.i18n.Tf(MsgWNDeployOption, strings.ToUpper(choice.environment), target)
	if choice.context != "" {
		label += " · " + choice.context
	}
	return label
}
