package osargs

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSandboxDelete(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want SandboxDelete
		bad  bool
	}{
		{name: "single", args: []string{"demo"}, want: SandboxDelete{Names: []string{"demo"}}},
		{name: "multiple", args: []string{"one", "two"}, want: SandboxDelete{Names: []string{"one", "two"}}},
		{name: "all", args: []string{"--all"}, want: SandboxDelete{All: true}},
		{name: "empty", bad: true},
		{name: "all with name", args: []string{"--all", "demo"}, bad: true},
		{name: "name with all", args: []string{"demo", "--all"}, bad: true},
		{name: "unknown flag", args: []string{"--force"}, bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSandboxDelete(tt.args)
			if (err != nil) != tt.bad {
				t.Fatalf("ParseSandboxDelete() error = %v, want error %v", err, tt.bad)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseSandboxDelete() = %#v, want %#v", got, tt.want)
			}
		})
	}
	if _, err := ParseSandboxDelete(nil); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("empty args error = %v, want usage error", err)
	}
}

func TestParseSandboxTarget(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
		bad  bool
	}{
		{want: ""},
		{args: []string{"demo"}, want: "demo"},
		{args: []string{"--all"}, bad: true},
		{args: []string{"one", "two"}, bad: true},
	} {
		got, err := ParseSandboxTarget(tt.args, "start")
		if (err != nil) != tt.bad || (!tt.bad && got != tt.want) {
			t.Errorf("ParseSandboxTarget(%v) = %q, %v; want %q, bad=%v", tt.args, got, err, tt.want, tt.bad)
		}
	}
}
