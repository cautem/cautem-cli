package service

import (
	"encoding/json"
	"testing"

	"github.com/cautem/quarshell-providers/provider"
	"github.com/cautem/quarshell-proxy/proxy"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

func TestSandboxTokenGrantsPackagesAttachedProfileMetadata(t *testing.T) {
	sandbox := cautem.Sandbox{AttachedProviders: []string{"corp"}}
	providers := map[string]cautem.ProviderRecord{
		"corp": {Name: "corp", Type: "corp-api", Workspace: "team"},
	}
	profiles := map[string]provider.Profile{
		"corp-api": {Credentials: []provider.Credential{{
			Name: "DYNAMIC_CREDENTIAL", EnvVars: []string{"DYNAMIC_TOKEN"},
			TokenGrant: &provider.TokenGrant{
				GrantType: "token_exchange", TokenEndpoint: "https://issuer.example/token",
				Audience: "https://api.example.com", JWTSVIDAudience: "https://issuer.example",
				Scopes: []string{"read"}, SubjectToken: &provider.SubjectToken{Source: "provider_credential", Credential: "UPSTREAM_TOKEN"},
			},
		}}},
	}
	raw := encodeSandboxTokenGrants(sandbox, providers, profiles)
	if raw == "" {
		t.Fatal("expected grant metadata")
	}
	var grants map[string]proxy.TokenGrantCredential
	if err := json.Unmarshal([]byte(raw), &grants); err != nil {
		t.Fatal(err)
	}
	g, ok := grants["DYNAMIC_TOKEN"]
	if !ok {
		t.Fatalf("grant keys=%v", grants)
	}
	if g.Provider != "corp" || g.CredentialKey != "DYNAMIC_CREDENTIAL" || g.SubjectTokenCredential != "UPSTREAM_TOKEN" || g.TokenEndpoint != "https://issuer.example/token" {
		t.Fatalf("grant metadata=%+v", g)
	}
}
