package core

import (
	"reflect"
	"testing"
)

func TestNotifierMultiChoice(t *testing.T) {
	f := notifierField{Type: "lines", Required: true, Options: []string{"a", "b", "c"}}
	for _, input := range []string{"1,3", "1，3", "1 3"} {
		got, err := parseNotifierInput(f, input)
		if err != nil || !reflect.DeepEqual(got, []string{"a", "c"}) {
			t.Fatalf("%s: %v %v", input, got, err)
		}
	}
	for _, input := range []string{"", "0", "4", "1,1", "abc", "1,4"} {
		if _, err := parseNotifierInput(f, input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestNotifierRepositoryScopedDefaults(t *testing.T) {
	for _, branch := range []string{"frontend/ui", "backend/ui"} {
		f := notifierField{Type: "text", Required: true, Options: []string{branch}, DefaultValue: branch}
		got, err := parseNotifierInput(f, "默认")
		if err != nil || got != branch {
			t.Fatalf("repository default: got %v, err %v", got, err)
		}
		if _, err := parseNotifierInput(f, "other/ui"); err == nil {
			t.Fatal("accepted a branch outside the bound repository options")
		}
	}
	if _, err := parseNotifierInput(notifierField{Type: "text", Required: true, Options: []string{}}, "1"); err == nil {
		t.Fatal("accepted an option for an unconfigured repository")
	}
}
