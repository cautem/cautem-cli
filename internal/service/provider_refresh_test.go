package service

import (
	"testing"

	"github.com/cautem/quarshell-providers/provider"
)

func TestProfileRefreshConfigMapsSecretsAndOutputs(t *testing.T) {
	profile := provider.Profile{
		ID: "sample",
		Credentials: []provider.Credential{
			{
				Name: "access_token", EnvVars: []string{"ACCESS_TOKEN"},
				Refresh: &provider.CredentialRefresh{
					Strategy: "oauth2_refresh_token", TokenURL: "https://auth.example/token",
					Scopes: []string{"read", "write"}, RefreshBeforeSeconds: 120,
					Material: []provider.RefreshMaterial{{Name: "refresh_token", Required: true}},
					AdditionalOutputs: []provider.RefreshOutput{
						{Output: "refresh_token", Credential: "refresh_token"},
						{Output: "id_token", Credential: "identity"},
					},
				},
			},
			{Name: "refresh_token", EnvVars: []string{"REFRESH_TOKEN"}},
			{Name: "identity", EnvVars: []string{"ID_TOKEN"}},
		},
	}
	got := profileRefreshConfig(profile, []string{"ACCESS_TOKEN", "REFRESH_TOKEN", "ID_TOKEN"})
	cfg, ok := got["ACCESS_TOKEN"]
	if !ok {
		t.Fatalf("refresh config not generated: %#v", got)
	}
	if cfg.Strategy != "oauth2-refresh-token" || cfg.Material["token_url"] != "https://auth.example/token" || cfg.Material["scope"] != "read write" {
		t.Fatalf("unexpected refresh config: %#v", cfg)
	}
	if cfg.MaterialCredentialKeys["refresh_token"] != "REFRESH_TOKEN" {
		t.Fatalf("refresh token was not linked to encrypted credential storage: %#v", cfg.MaterialCredentialKeys)
	}
	want := map[string]string{"access_token": "ACCESS_TOKEN", "refresh_token": "REFRESH_TOKEN", "id_token": "ID_TOKEN"}
	for output, key := range want {
		if cfg.Outputs[output] != key {
			t.Errorf("output %q maps to %q, want %q", output, cfg.Outputs[output], key)
		}
	}
}

func TestProfileRefreshConfigDoesNotActivateUnsupportedExecutor(t *testing.T) {
	profile := provider.Profile{
		ID: "sample",
		Credentials: []provider.Credential{{
			Name: "token", EnvVars: []string{"TOKEN"},
			Refresh: &provider.CredentialRefresh{Strategy: "google_service_account_jwt"},
		}},
	}
	if got := profileRefreshConfig(profile, []string{"TOKEN"}); got != nil {
		t.Fatalf("unsupported executor should not be activated: %#v", got)
	}
}
