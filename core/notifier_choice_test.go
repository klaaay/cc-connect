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
