package antigravity

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

type agyModelCatalog struct {
	models  []core.ModelOption
	efforts map[string][]string
}

var effortOrder = []string{"low", "medium", "high", "xhigh", "max"}

// Use the installed CLI's catalog, including its configured endpoint and credentials.
// Group CLI variants into model families so model and effort can be selected separately.
func parseModelCatalog(output string) *agyModelCatalog {
	c := &agyModelCatalog{efforts: map[string][]string{}}
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		columns := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(columns) != 2 || columns[0] == "" {
			continue
		}
		name, desc, effort := columns[0], strings.TrimSpace(columns[1]), ""
		for _, e := range effortOrder {
			if strings.HasSuffix(name, "-"+e) {
				name = strings.TrimSuffix(name, "-"+e)
				effort = e
				if i := strings.LastIndex(desc, " ("); i >= 0 {
					desc = desc[:i]
				}
				break
			}
		}
		if !seen[name] {
			c.models = append(c.models, core.ModelOption{Name: name, Desc: desc})
			seen[name] = true
		}
		if effort != "" && !hasEffort(c.efforts[name], effort) {
			c.efforts[name] = append(c.efforts[name], effort)
		}
	}
	for model, values := range c.efforts {
		ordered := []string{}
		for _, e := range effortOrder {
			for _, v := range values {
				if e == v {
					ordered = append(ordered, e)
				}
			}
		}
		c.efforts[model] = ordered
	}
	return c
}

func (a *Agent) cliOutput(ctx context.Context, args ...string) ([]byte, error) {
	a.mu.RLock()
	binary, dir := a.cmd, a.workDir
	argv := append(append([]string{}, a.cliExtraArgs...), args...)
	env := core.MergeEnv(os.Environ(), append(append([]string{}, a.configEnv...), a.providerEnvLocked()...))
	a.mu.RUnlock()
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Env, cmd.Dir = env, dir
	cmd.WaitDelay = time.Second
	output, err := cmd.Output()
	// Never include subprocess output or environment in authentication errors.
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return output, errors.New("antigravity: CLI installation or authentication unavailable")
	}
	return output, nil
}

func (a *Agent) loadModelCatalog(ctx context.Context) *agyModelCatalog {
	a.catalogMu.Lock()
	defer a.catalogMu.Unlock()
	if a.catalog != nil && time.Since(a.catalogAt) < time.Minute {
		return a.catalog
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := a.cliOutput(ctx, "models")
	if err != nil {
		return &agyModelCatalog{efforts: map[string][]string{}}
	}
	c := parseModelCatalog(string(output))
	if len(c.models) > 0 {
		a.catalog = c
		a.catalogAt = time.Now()
	}
	return c
}

func (a *Agent) AvailableModels(ctx context.Context) []core.ModelOption {
	if models := a.configuredModels(); len(models) > 0 {
		return models
	}
	return append([]core.ModelOption(nil), a.loadModelCatalog(ctx).models...)
}
func (a *Agent) ModelReasoningEfforts(ctx context.Context) map[string][]string {
	c := a.loadModelCatalog(ctx)
	result := make(map[string][]string, len(c.efforts))
	for key, values := range c.efforts {
		result[key] = append([]string(nil), values...)
	}
	return result
}
func (a *Agent) SetReasoningEffort(effort string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reasoningEffort = strings.ToLower(strings.TrimSpace(effort))
}
func (a *Agent) GetReasoningEffort() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.reasoningEffort
}
func (a *Agent) AvailableReasoningEfforts() []string {
	c := a.loadModelCatalog(context.Background())
	model := a.GetModel()
	for _, e := range effortOrder {
		model = strings.TrimSuffix(model, "-"+e)
	}
	if model != "" {
		return append([]string(nil), c.efforts[model]...)
	}
	// The default model is selected by agy settings: offer only shared efforts.
	result := []string{}
	for _, e := range effortOrder {
		shared := len(c.models) > 0
		for _, m := range c.models {
			if !hasEffort(c.efforts[m.Name], e) {
				shared = false
				break
			}
		}
		if shared {
			result = append(result, e)
		}
	}
	return result
}
func hasEffort(values []string, effort string) bool {
	for _, value := range values {
		if value == effort {
			return true
		}
	}
	return false
}

// /model is handled locally by agy and performs no model turn. It confirms that
// the CLI can load its selected provider/credentials, not remote API authorization.
func (a *Agent) MaintenanceReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := a.cliOutput(ctx, "--print", "/model")
	if err != nil {
		return err
	}
	if len(parseModelCatalog(string(output)).models) == 0 {
		return errors.New("antigravity: model or authentication configuration unavailable")
	}
	return nil
}

func (a *Agent) invalidateCatalog() {
	a.catalogMu.Lock()
	defer a.catalogMu.Unlock()
	a.catalog = nil
}
