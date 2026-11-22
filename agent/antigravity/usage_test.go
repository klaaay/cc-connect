package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestParseQuotaGroups(t *testing.T) {
	const payload = `{"command":{"name":"usage","data":{"groups":[
		{"name":"Gemini Models","buckets":[{"window":"weekly","remaining_fraction":0.9998,"reset_time":"2026-11-23T05:00:00Z"},{"window":"5h","remaining_fraction":0}]},
		{"name":"Claude and GPT models","buckets":[{"window":"weekly","remaining_fraction":1,"reset_time":"1970-01-01T00:00:00Z"},{"window":"5h"}]}
	]}}}`
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	report, err := parseQuota([]byte(payload), now)
	if err != nil || !report.Grouped || len(report.Buckets) != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	first, second := report.Buckets[0], report.Buckets[1]
	if first.Name != "Gemini Models" || len(first.Windows) != 2 || *first.Windows[0].RemainingPercent != 99.98 || first.Windows[0].WindowSeconds != 604800 || first.Windows[0].ResetAfterSeconds != 18000 || *first.Windows[1].RemainingPercent != 0 {
		t.Fatalf("first=%+v", first)
	}
	if len(second.Windows) != 1 || *second.Windows[0].RemainingPercent != 100 || second.Windows[0].ResetAfterSeconds != 0 {
		t.Fatalf("second=%+v", second)
	}
	if _, err := parseQuota([]byte(`{"command":{"name":"usage"}}`), now); err == nil {
		t.Fatal("accepted missing quota data")
	}
}

func TestQueryUsageRejectsAPIKey(t *testing.T) {
	dir := t.TempDir()
	cmd := filepath.Join(dir, "agy")
	if err := os.WriteFile(cmd, []byte(`#!/bin/sh
[ "$*" = "-p /usage --output-format json" ] || exit 1
echo '{"error":"/usage failed: not available with a Gemini API key; see provider dashboard"}'
exit 2
`), 0700); err != nil {
		t.Fatal(err)
	}
	a := &Agent{cmd: cmd, workDir: dir, activeIdx: -1}
	if _, err := a.QueryUsage(context.Background()); !errors.Is(err, core.ErrUsageAuthUnsupported) {
		t.Fatalf("unexpected error: %v", err)
	}
}
