package cli

import (
	"strings"
	"testing"
)

func TestSettingsGetRejectsExtraArguments(t *testing.T) {
	err := runSettings(nil, []string{"get", "theme", "editor"})
	if err == nil || !strings.Contains(err.Error(), `unexpected argument "editor"`) {
		t.Fatalf("error=%v, want extra setting name to be rejected", err)
	}
}

func TestSettingsDeleteRejectsMalformedArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"delete", "--unknown"}, want: `unknown flag "--unknown"`},
		{name: "missing key value", args: []string{"delete", "--key"}, want: "--key needs a value"},
		{name: "duplicate key", args: []string{"delete", "--key", "theme", "--key", "editor"}, want: "settings key specified more than once"},
		{name: "extra positional argument", args: []string{"delete", "theme", "editor"}, want: `unexpected argument "editor"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runSettings(nil, test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestWorkspaceAndServiceCommandsRejectExtraArguments(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "workspace get", run: func() error { return runWorkspace(nil, []string{"get", "team-a", "extra"}) }, want: `unexpected argument "extra"`},
		{name: "workspace member add", run: func() error { return runWorkspace(nil, []string{"member", "add", "team-a", "alice", "extra"}) }, want: `unexpected argument "extra"`},
		{name: "service get", run: func() error { return runService(nil, []string{"get", "sandbox", "api", "extra"}) }, want: `unexpected argument "extra"`},
		{name: "service delete", run: func() error { return runService(nil, []string{"delete", "sandbox", "api", "extra"}) }, want: `unexpected argument "extra"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestWorkspaceDeleteAcceptsMultipleNames(t *testing.T) {
	names, err := parseWorkspaceDeleteNames([]string{"team-a", "team-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "team-a" || names[1] != "team-b" {
		t.Fatalf("names=%q, want both workspace names", names)
	}
}

func TestWorkspaceDeleteRejectsMissingOrFlagLikeNames(t *testing.T) {
	for _, args := range [][]string{nil, {""}, {"--unknown"}} {
		if _, err := parseWorkspaceDeleteNames(args); err == nil {
			t.Errorf("parseWorkspaceDeleteNames(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParseWorkspaceCreateArgsParsesRepeatedLabels(t *testing.T) {
	name, labels, err := parseWorkspaceCreateArgs([]string{
		"--name=team-a", "--label", "env=dev", "--label=owner=agents", "--label", "env=staging", "--label", "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "team-a" {
		t.Fatalf("name=%q, want team-a", name)
	}
	if len(labels) != 2 || labels["env"] != "staging" || labels["owner"] != "agents" {
		t.Fatalf("labels=%v, want last value per valid KEY=VALUE", labels)
	}
}

func TestParseWorkspaceCreateArgsRejectsDuplicateOrMissingName(t *testing.T) {
	for _, args := range [][]string{
		{"--name", "one", "--name", "two"},
		{"--name", "--label", "env=test"},
		{"--label", "env=test"},
	} {
		if _, _, err := parseWorkspaceCreateArgs(args); err == nil {
			t.Errorf("parseWorkspaceCreateArgs(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParseWorkspaceListArgsSupportsPinnedPaginationFlags(t *testing.T) {
	options, err := parseWorkspaceListArgs([]string{
		"--page-size=25", "--page-token", "cursor-1", "--label-selector=env=staging", "-o", "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.PageSize != 25 || options.PageToken != "cursor-1" || options.LabelSelector != "env=staging" || options.Output != "json" {
		t.Fatalf("options=%+v", options)
	}
	yamlOptions, err := parseWorkspaceListArgs([]string{"--output=yaml"})
	if err != nil || yamlOptions.Output != "yaml" {
		t.Fatalf("yaml options=%+v, error=%v", yamlOptions, err)
	}
	defaults, err := parseWorkspaceListArgs(nil)
	if err != nil || defaults.PageSize != 100 {
		t.Fatalf("default options=%+v, error=%v", defaults, err)
	}
}

func TestParseWorkspaceListArgsRejectsMalformedAndDuplicateFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--page-size", "0"},
		{"--page-size", "2147483648"},
		{"--page-size", "10", "--page-size", "20"},
		{"--page-token"},
		{"--output", "xml"},
		{"unexpected"},
	} {
		if _, err := parseWorkspaceListArgs(args); err == nil {
			t.Errorf("parseWorkspaceListArgs(%q) unexpectedly succeeded", args)
		}
	}
}
