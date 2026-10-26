package core

import (
	"context"
	"os"
	"testing"
)

// 显式启用的只读联调，不投递任务或部署，不输出业务内容和凭据。
func TestNotifierLiveV2ReadOnly(t *testing.T) {
	base, token := os.Getenv("NOTIFIER_TEST_BASE_URL"), os.Getenv("NOTIFIER_TEST_TOKEN_FILE")
	if base == "" || token == "" {
		t.Skip("live Notifier not configured")
	}
	c, err := newNotifierClient(NotifierConfig{BaseURL: base, TokenFile: token})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := c.catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runs, err := c.runs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("v%d catalog: tasks=%d deployments=%d runs=%d", catalog.Version, len(catalog.Tasks), len(catalog.Deployments), len(runs))
}
