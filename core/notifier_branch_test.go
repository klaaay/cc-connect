package core

import (
	"strings"
	"testing"
)

func TestNotifierBranchMatchesReleaseContract(t *testing.T) {
	for _, branch := range []string{"main", "feature/v0.3.2", "fix/test-1"} {
		if !validNotifierBranch(branch) {
			t.Errorf("rejected %q", branch)
		}
	}
	for _, branch := range []string{"", "--help", "a..b", "a//b", "a/", "a/.hidden", "a.lock", "a/b.", "a:b", "a b", strings.Repeat("a", 201)} {
		if validNotifierBranch(branch) {
			t.Errorf("accepted %q", branch)
		}
	}
}
