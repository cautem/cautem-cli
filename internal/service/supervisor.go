package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cautem/cautem-core/policy"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-providers/provider"
	"github.com/cautem/cautem-proxy/proxy"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
)

// attachSupervisor wires the proxy sidecar as the sandbox supervisor
// (OpenShell model): the sandbox is registered with the gateway before the
// sidecar starts, a sandbox-scoped token is minted for the sidecar only, and
// the in-sandbox sshd is enabled so IDE / connect traffic flows through the
// gateway relay. Requires spec.ProxyBin.
func (a *App) attachSupervisor(ctx context.Context, spec *driver.Spec, doc policy.Document, name, gwURL string, attachedProviders ...[]string) error {
	spec.ProxyEnv = proxySecretsForPolicy(doc)
	if gwURL == "" {
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
	}
	if gwURL == "" {
		fmt.Fprintf(os.Stderr, "sandbox create: warn: no gateway selected; SSH / IDE access disabled\n")
		return nil
	}
	c := a.clientFor(gwURL)
	var attached []string
	if len(attachedProviders) > 0 {
		attached = attachedProviders[0]
	}
	if err := c.UpsertSandbox(ctx, cautem.Sandbox{Name: name, Image: spec.Image, Status: "creating", Labels: spec.Labels, AttachedProviders: attached}); err != nil {
		return fmt.Errorf("register sandbox with gateway %s: %w (run: cautem gateway login)", gwURL, err)
	}
	tok, err := c.IssueSandboxToken(ctx, name)
	if err != nil {
		return fmt.Errorf("sandbox supervisor token: %w", err)
	}
	spec.ProxyEnv = append(spec.ProxyEnv, a.proxyGatewayEnv(name, gwURL, tok)...)
	if encoded := sandboxTokenGrants(ctx, c, name, gwURL); encoded != "" {
		spec.ProxyEnv = append(spec.ProxyEnv, proxy.EnvTokenGrants+"="+encoded)
	}
	if socket := firstNonEmptyEnv("OPENSHELL_PROVIDER_SPIFFE_WORKLOAD_API_SOCKET", "CAUTEM_PROVIDER_SPIFFE_WORKLOAD_API_SOCKET"); socket != "" {
		spec.ProxyEnv = append(spec.ProxyEnv, "CAUTEM_PROVIDER_SPIFFE_WORKLOAD_API_SOCKET="+strings.TrimPrefix(socket, "unix://"))
	}
	sshBin, err := ensureSSHDBin(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox create: warn: cautem-sshd unavailable (%v); SSH / IDE access disabled\n", err)
		return nil
	}
	spec.SSHBin = sshBin
	spec.EnableSSH = true
	return nil
}

// sandboxTokenGrants packages only profile metadata for the proxy sidecar.
// Subject token values remain in the gateway's encrypted credential store.
func sandboxTokenGrants(ctx context.Context, c *cautem.Client, sandbox, gatewayURL string) string {
	sb, err := c.GetSandbox(ctx, sandbox)
	if err != nil {
		return ""
	}
	providers := make(map[string]cautem.ProviderRecord, len(sb.AttachedProviders))
	profiles := make(map[string]provider.Profile, len(sb.AttachedProviders))
	for _, name := range sb.AttachedProviders {
		rec, err := c.GetProvider(ctx, name)
		if err != nil {
			continue
		}
		profile, err := tokenGrantProviderProfile(ctx, c, rec.Type, rec.Workspace)
		if err != nil {
			continue
		}
		providers[name], profiles[rec.Type] = rec, profile
	}
	return encodeSandboxTokenGrants(sb, providers, profiles)
}

func encodeSandboxTokenGrants(sb cautem.Sandbox, providers map[string]cautem.ProviderRecord, profiles map[string]provider.Profile) string {
	grants := map[string]proxy.TokenGrantCredential{}
	for _, name := range sb.AttachedProviders {
		rec, ok := providers[name]
		if !ok {
			continue
		}
		profile, ok := profiles[rec.Type]
		if !ok {
			continue
		}
		for _, credential := range profile.Credentials {
			if credential.TokenGrant == nil {
				continue
			}
			grant := credential.TokenGrant
			for _, key := range credential.EnvVars {
				if key == "" {
					continue
				}
				out := proxy.TokenGrantCredential{Provider: rec.Name, CredentialKey: credential.Name, TokenEndpoint: grant.TokenEndpoint,
					GrantType: grant.GrantType, Audience: grant.Audience, JWTSVIDAudience: grant.JWTSVIDAudience,
					ClientAssertionType: grant.ClientAssertionType, RequestedTokenType: grant.RequestedTokenType,
					Scopes: append([]string(nil), grant.Scopes...)}
				if d, err := time.ParseDuration(grant.CacheTTL); err == nil && d > 0 {
					out.CacheTTLSeconds = int64(d / time.Second)
				}
				if grant.SubjectToken != nil {
					out.SubjectTokenType, out.SubjectTokenCredential = grant.SubjectToken.SubjectTokenType, grant.SubjectToken.Credential
				}
				for _, override := range grant.AudienceOverrides {
					out.AudienceOverrides = append(out.AudienceOverrides, proxy.TokenGrantAudienceOverride{Host: override.Host, Port: override.Port, Path: override.Path, Audience: override.Audience, Scopes: append([]string(nil), override.Scopes...)})
				}
				grants[key] = out
			}
		}
	}
	if len(grants) == 0 {
		return ""
	}
	b, err := json.Marshal(grants)
	if err != nil {
		return ""
	}
	return string(b)
}

func tokenGrantProviderProfile(ctx context.Context, c *cautem.Client, id, workspace string) (provider.Profile, error) {
	if workspace != "" {
		if raw, _, _, err := c.GetProfileScoped(ctx, id, "workspace", workspace); err == nil {
			if profile, parseErr := provider.ParseYAML(raw); parseErr == nil {
				return profile, nil
			}
		}
	}
	if raw, _, _, err := c.GetProfileScoped(ctx, id, "global", ""); err == nil {
		if profile, parseErr := provider.ParseYAML(raw); parseErr == nil {
			return profile, nil
		}
	}
	return loadBuiltinProfile(id)
}
