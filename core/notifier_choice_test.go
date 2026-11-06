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

func TestNotifierTextDefaultRegression(t *testing.T) {
	for _, value := range []string{"wuzhen01", "default", "默认", ""} {
		f := notifierField{Type: "text", DefaultValue: value}
		got, err := parseNotifierInput(f, "default")
		if err != nil || got != value {
			t.Fatalf("default %q: %v %v", value, got, err)
		}
	}
	f := notifierField{Type: "text", MaxLength: 2, DefaultValue: "long"}
	if _, err := parseNotifierInput(f, "default"); err == nil {
		t.Fatal("invalid default accepted")
	}
}

func TestNotifierOptionalTextClearAndUsername(t *testing.T) {
	for _, input := range []string{"清空", "clear", "CLEAR"} {
		got, err := parseNotifierInput(notifierField{Type: "text", Format: "gitlab-username"}, input)
		if err != nil || got != "" {
			t.Fatalf("clear: %v %v", got, err)
		}
	}
	for _, value := range []string{"bad name", "-bad", "bad\nname"} {
		if _, err := parseNotifierInput(notifierField{Type: "text", Format: "gitlab-username", DefaultValue: value}, "default"); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	f := notifierField{Type: "text", Options: []string{"ui"}, DefaultValue: "other"}
	if _, err := parseNotifierInput(f, "default"); err == nil {
		t.Fatal("accepted stale branch default")
	}
	if _, err := parseNotifierInput(f, "clear"); err == nil {
		t.Fatal("cleared branch selector")
	}
	for _, value := range []bool{true, false} {
		got, err := parseNotifierInput(notifierField{Type: "boolean", DefaultValue: value}, "default")
		if err != nil || got != value {
			t.Fatalf("boolean default: %v %v", got, err)
		}
	}
}
