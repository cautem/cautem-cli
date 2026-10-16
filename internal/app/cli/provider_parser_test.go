package cli

import (
	"strings"
	"testing"
)

func TestParseProfileIOAcceptsOneSourceAndOneOutputFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		file bool
		want string
		fmt  string
	}{
		{name: "positional ID", args: []string{"codex"}, want: "codex", fmt: "yaml"},
		{name: "file option", args: []string{"--file", "provider.yaml"}, file: true, want: "provider.yaml", fmt: "yaml"},
		{name: "URL option", args: []string{"--url=https://example.test/provider.yaml"}, file: true, want: "https://example.test/provider.yaml", fmt: "yaml"},
		{name: "ID with JSON output", args: []string{"codex", "-o", "JSON"}, want: "codex", fmt: "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, format, err := parseProfileIO(tc.args, tc.file)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || format != tc.fmt {
				t.Fatalf("parseProfileIO() = (%q, %q), want (%q, %q)", got, format, tc.want, tc.fmt)
			}
		})
	}
}

func TestParseProfileIORejectsAmbiguousOrIgnoredArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		file bool
	}{
		{name: "extra positional", args: []string{"codex", "ignored"}},
		{name: "two sources", args: []string{"--file", "one.yaml", "--url", "https://example.test/two.yaml"}, file: true},
		{name: "duplicate output", args: []string{"codex", "-o", "json", "--output", "yaml"}},
		{name: "unsupported output", args: []string{"codex", "-o", "toml"}},
		{name: "output on import", args: []string{"-f", "provider.yaml", "-o", "json"}, file: true},
		{name: "missing source value", args: []string{"--file"}, file: true},
		{name: "empty source value", args: []string{"--file="}, file: true},
		{name: "unknown flag", args: []string{"codex", "--force"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseProfileIO(tc.args, tc.file); err == nil {
				t.Fatalf("parseProfileIO(%q) unexpectedly succeeded", tc.args)
			}
		})
	}
}

func TestProviderCommandsRejectExtraPositionalArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"list", "ignored"}},
		{name: "get", args: []string{"get", "github", "ignored"}},
		{name: "delete", args: []string{"delete", "github", "ignored"}},
		{name: "refresh status", args: []string{"refresh", "status", "github", "ignored"}},
		{name: "refresh", args: []string{"refresh", "github", "ignored"}},
		{name: "effective", args: []string{"effective", "sandbox", "ignored"}},
		{name: "profile show", args: []string{"profile", "show", "github", "ignored"}},
		{name: "profile import", args: []string{"profile", "import", "-f", "provider.yaml", "ignored"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runProvider(nil, tc.args)
			if err == nil {
				t.Fatalf("runProvider(%q) unexpectedly succeeded", tc.args)
			}
			if !strings.Contains(err.Error(), "usage:") && !strings.Contains(err.Error(), "extra argument") {
				t.Fatalf("runProvider(%q) error = %q, want usage or extra-argument error", tc.args, err)
			}
		})
	}
}
