package providerflags_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/providerflags"
)

func TestParseCreateCompat(t *testing.T) {
	a, err := providerflags.ParseCreate([]string{"--name", "gh", "--type", "github", "--credential", "GITHUB_TOKEN"}, func(k string) (string, bool) {
		if k == "GITHUB_TOKEN" {
			return "tok", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "gh" || a.Profile != "github" || a.Credentials["GITHUB_TOKEN"] != "tok" {
		t.Fatalf("%+v", a)
	}
}

func TestParseCreateFromExisting(t *testing.T) {
	a, err := providerflags.ParseCreate([]string{"--name", "my-claude", "--type", "claude-code", "--from-existing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "my-claude" || a.Profile != "claude-code" || !a.FromExisting {
		t.Fatalf("%+v", a)
	}
}

func TestParseCreateRejectsPositional(t *testing.T) {
	if _, err := providerflags.ParseCreate([]string{"gh", "--type", "github"}, nil); err == nil {
		t.Fatal("expected error for positional name")
	}
}

func TestParseCreateRequiresType(t *testing.T) {
	if _, err := providerflags.ParseCreate([]string{"--name", "x"}, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCreateOIDCAndExpiry(t *testing.T) {
	a, err := providerflags.ParseCreate([]string{
		"--name", "gw", "--type", "cursor",
		"--from-oidc-token", "--runtime-credentials",
		"--credential-expires-at", "API_KEY=1700000000000",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !a.FromOIDCToken || !a.RuntimeCredentials {
		t.Fatalf("%+v", a)
	}
	if a.CredentialExpiresAt["API_KEY"] != 1700000000000 {
		t.Fatalf("expiry %v", a.CredentialExpiresAt)
	}
}

func TestParseCreateMatchesRepeatedMapValuePrecedence(t *testing.T) {
	a, err := providerflags.ParseCreate([]string{
		"--name", "gw", "--type", "cursor",
		"--config", "SCOPE=first", "--config", "SCOPE=last",
		"--credential", "API_KEY=first", "--credential", "API_KEY=last",
		"--credential-expires-at", "API_KEY=100", "--credential-expires-at", "API_KEY=200",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Config["SCOPE"] != "last" || a.Credentials["API_KEY"] != "last" || a.CredentialExpiresAt["API_KEY"] != 200 {
		t.Fatalf("repeated KEY=VALUE fields did not preserve last-value behavior: %+v", a)
	}
}

func TestParseCredentialExpiryUsesMillisecondsAndRejectsNegative(t *testing.T) {
	a, err := providerflags.ParseCreate([]string{
		"--name", "gw", "--type", "cursor", "--credential-expires-at", "API_KEY=1700000000",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.CredentialExpiresAt["API_KEY"]; got != 1700000000 {
		t.Fatalf("expiry = %d ms, want 1700000000 ms", got)
	}
	if _, err := providerflags.ParseCreate([]string{
		"--name", "gw", "--type", "cursor", "--credential-expires-at", "API_KEY=-1",
	}, nil); err == nil {
		t.Fatal("negative credential expiry was accepted")
	}
}

func TestParseRefreshConfigure(t *testing.T) {
	opt, err := providerflags.ParseRefreshConfigure("gh", []string{
		"--credential-key", "GITHUB_TOKEN", "--strategy", "env",
		"--material", "SCOPE=repo", "--credential-expires-at", "999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opt.CredKey != "GITHUB_TOKEN" || opt.Strategy != "env" || opt.Material["SCOPE"] != "repo" || opt.ExpiresAtMS != 999 {
		t.Fatalf("%+v", opt)
	}
}

func TestParseRefreshConfigureMatchesSingleFlagContract(t *testing.T) {
	if _, err := providerflags.ParseRefreshConfigure("gh", []string{"--credential-key", "TOKEN", "--credential-expires-at", "TOKEN=100"}); err == nil {
		t.Fatal("KEY=TIMESTAMP expiry form was accepted for refresh configure")
	}
	if _, err := providerflags.ParseRefreshConfigure("gh", []string{"--credential-key", "TOKEN"}); err == nil {
		t.Fatal("missing required --strategy was accepted")
	}
	if _, err := providerflags.ParseRefreshConfigure("gh", []string{"--credential-key", "TOKEN", "--strategy", "env", "--credential-key", "OTHER"}); err == nil {
		t.Fatal("duplicate --credential-key was accepted")
	}
	if _, err := providerflags.ParseRefreshKey("gh", []string{"--credential-key", "TOKEN", "--credential-key", "OTHER"}); err == nil {
		t.Fatal("duplicate --credential-key for rotate/delete was accepted")
	}
}
