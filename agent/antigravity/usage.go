package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func (a *Agent) QueryUsage(ctx context.Context) (*core.UsageReport, error) {
	output, err := a.cliOutput(ctx, "-p", "/usage", "--output-format", "json")
	if err != nil {
		var failure struct{ Error string }
		if json.Unmarshal(output, &failure) == nil && strings.Contains(failure.Error, "/usage failed: not available with a Gemini API key") {
			return nil, core.ErrUsageAuthUnsupported
		}
		return nil, err
	}
	return parseQuota(output, time.Now())
}

func parseQuota(output []byte, now time.Time) (*core.UsageReport, error) {
	var payload struct {
		Command struct {
			Name string
			Data struct {
				Groups []struct {
					Name    string
					Buckets []struct {
						Window    string
						Remaining *float64  `json:"remaining_fraction"`
						ResetTime time.Time `json:"reset_time"`
					}
				}
			}
		}
	}
	if json.Unmarshal(output, &payload) != nil || payload.Command.Name != "usage" {
		return nil, errors.New("antigravity: invalid /usage response")
	}
	report := &core.UsageReport{Provider: "antigravity", Grouped: true}
	for _, group := range payload.Command.Data.Groups {
		bucket := core.UsageBucket{Name: group.Name}
		for _, quota := range group.Buckets {
			if quota.Remaining == nil || *quota.Remaining < 0 || *quota.Remaining > 1 {
				continue
			}
			percent := *quota.Remaining * 100
			w := core.UsageWindow{RemainingPercent: &percent}
			switch quota.Window {
			case "weekly":
				w.WindowSeconds = 7 * 24 * 3600
			case "5h":
				w.WindowSeconds = 5 * 3600
			default:
				continue
			}
			if quota.ResetTime.After(now) {
				w.ResetAtUnix = quota.ResetTime.Unix()
				w.ResetAfterSeconds = int(quota.ResetTime.Sub(now).Seconds())
			}
			bucket.Windows = append(bucket.Windows, w)
		}
		if len(bucket.Windows) > 0 {
			report.Buckets = append(report.Buckets, bucket)
		}
	}
	if len(report.Buckets) == 0 {
		return nil, errors.New("antigravity: no quota windows in /usage response")
	}
	return report, nil
}
