package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cautem/cautem-core/engine"
	"github.com/cautem/cautem-core/policy"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/policywait"
	"github.com/cautem/slogx"
	"gopkg.in/yaml.v3"
)

// PolicyCheck loads and validates a policy YAML file.
func (a *App) PolicyCheck(path string) error {
	const op = "cli.policy.check"
	log := a.op(op, slog.String("path", path))
	log.Info("checking policy")
	doc, err := policy.Load(path)
	if err != nil {
		log.Error("failed to load policy", slogx.Err(err))
		return fmt.Errorf("policy check: %w", err)
	}
	doc, err = a.mergeGatewayGlobal(doc)
	if err != nil {
		log.Error("failed to merge global policy", slogx.Err(err))
		return fmt.Errorf("policy check: %w", err)
	}
	if err := doc.Validate(); err != nil {
		log.Error("policy validation failed", slogx.Err(err))
		return fmt.Errorf("policy check: %w", err)
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		log.Error("policy engine apply failed", slogx.Err(err))
		return fmt.Errorf("policy check: engine: %w", err)
	}
	log.Info("policy check ok", slog.Int("allow_rules", len(doc.AllowRules())), slog.String("harden", doc.HardenMode()))
	fmt.Printf("policy check: ok version=%d harden=%s allow_rules=%d include_workdir=%v",
		doc.Version, doc.HardenMode(), len(doc.AllowRules()),
		doc.IncludeWorkdir())
	if doc.Inference != nil && len(doc.Inference.Providers) > 0 {
		fmt.Printf(" inference=%v", doc.Inference.Providers)
	}
	if len(doc.Binaries) > 0 {
		fmt.Printf(" binaries=%d", len(doc.Binaries))
	}
	fmt.Println()
	return nil
}

// PolicyGlobalGet prints gateway global policy YAML.

// PolicyGlobalGet prints gateway global policy YAML.
func (a *App) PolicyGlobalGet() error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	b, err := cautem.NewWithToken(u, a.gatewayTokenForURL(u)).GetGlobalPolicy(a.apiCtx())
	if err != nil {
		return err
	}
	os.Stdout.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// PolicyGlobalSet uploads YAML file as gateway global policy.

// PolicyGlobalSet uploads YAML file as gateway global policy.
func (a *App) PolicyGlobalSet(path string) error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	if err := cautem.NewWithToken(u, a.gatewayTokenForURL(u)).PutGlobalPolicy(a.apiCtx(), b); err != nil {
		return err
	}
	fmt.Printf("policy global set: ok url=%s bytes=%d\n", u, len(b))
	return nil
}

// PolicyGlobalClear removes the gateway-global policy lock.

// PolicyGlobalClear removes the gateway-global policy lock.
func (a *App) PolicyGlobalClear() error {
	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	if err := cautem.NewWithToken(u, a.gatewayTokenForURL(u)).PutGlobalPolicy(a.apiCtx(), nil); err != nil {
		return err
	}
	fmt.Println("policy delete --global: ok")
	return nil
}

// PolicySet stores sandbox base policy; the gateway builds the effective
// candidate via composition (base + provider-composed profiles) — OpenShell-style.
//
// Requires the sandbox to be registered on the current gateway (create/register).
// Without a gateway, falls back to writing the file directly (no composition).
// When wait is true, blocks until the bind file matches and settle elapsed.

// PolicySet stores sandbox base policy; the gateway builds the effective
// candidate via composition (base + provider-composed profiles) — OpenShell-style.
//
// Requires the sandbox to be registered on the current gateway (create/register).
// Without a gateway, falls back to writing the file directly (no composition).
// When wait is true, blocks until the bind file matches and settle elapsed.
func (a *App) PolicySet(sandboxName, path string, wait bool) error {
	const op = "cli.policy.set"
	log := a.op(op, slog.String("sandbox", sandboxName), slog.String("path", path), slog.Bool("wait", wait))
	log.Info("setting sandbox policy")
	b, err := os.ReadFile(path)
	if err != nil {
		log.Error("failed to read policy file", slogx.Err(err))
		return err
	}
	doc, err := policy.Parse(b)
	if err != nil {
		log.Error("failed to parse policy", slogx.Err(err))
		return fmt.Errorf("policy set: %w", err)
	}
	if err := doc.Validate(); err != nil {
		log.Error("policy validation failed", slogx.Err(err))
		return fmt.Errorf("policy set: %w", err)
	}

	var appliedHost string
	var appliedBytes []byte

	if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
		ctx, cancel := a.withTimeout(TimeoutAPILong)
		defer cancel()
		if _, err := c.Healthz(ctx); err == nil {
			eff, stripped, err := c.PutSandboxPolicy(ctx, sandboxName, b)
			if err != nil {
				return fmt.Errorf("policy set: %w", err)
			}
			if stripped > 0 {
				fmt.Printf("policy set: stripped %d provider-composed rule(s) from base (prefer policy get --base)\n", stripped)
			}
			effDoc, err := policy.Parse(eff)
			if err != nil {
				return fmt.Errorf("policy set: effective: %w", err)
			}
			var eng engine.Allowlist
			if err := eng.Apply(effDoc); err != nil {
				return fmt.Errorf("policy set: engine: %w", err)
			}
			if a.Docker != nil {
				hostPath, err := a.Docker.PolicyHostPath(ctx, sandboxName)
				if err != nil {
					return fmt.Errorf("policy set: gateway stored base, but live bind: %w (run: cautem provider effective %s | …)", err, sandboxName)
				}
				if err := writeFileInPlace(hostPath, eff); err != nil {
					return fmt.Errorf("policy set: write %s: %w", hostPath, err)
				}
				appliedHost, appliedBytes = hostPath, eff
				fmt.Printf("policy set: ok sandbox=%s base stored; effective policy applied (rules=%d) host=%s\n",
					sandboxName, len(effDoc.AllowRules()), hostPath)
			} else {
				fmt.Printf("policy set: ok sandbox=%s base stored; effective policy ready (rules=%d, no docker bind)\n",
					sandboxName, len(effDoc.AllowRules()))
			}
			if wait {
				return a.waitPolicy(appliedHost, appliedBytes)
			}
			return nil
		}
	}

	// No gateway: overwrite bind file as-is (no composition).
	fmt.Fprintln(os.Stderr, "policy set: warn: no gateway — writing file directly (no provider composition)")
	doc, err = a.mergeGatewayGlobal(doc)
	if err != nil {
		return fmt.Errorf("policy set: %w", err)
	}
	var eng engine.Allowlist
	if err := eng.Apply(doc); err != nil {
		return fmt.Errorf("policy set: engine: %w", err)
	}
	if a.Docker == nil {
		return fmt.Errorf("policy set: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()
	hostPath, err := a.Docker.PolicyHostPath(ctx, sandboxName)
	if err != nil {
		return err
	}
	if err := writeFileInPlace(hostPath, b); err != nil {
		return fmt.Errorf("policy set: write %s: %w", hostPath, err)
	}
	fmt.Printf("policy set: ok sandbox=%s host=%s allow_rules=%d (proxy reloads within ~1s)\n",
		sandboxName, hostPath, len(doc.AllowRules()))
	if wait {
		return a.waitPolicy(hostPath, b)
	}
	return nil
}

func (a *App) waitPolicy(hostPath string, want []byte) error {
	if hostPath == "" || len(want) == 0 {
		fmt.Println("policy set: --wait skipped (no live bind path)")
		return nil
	}
	ctx, cancel := a.withTimeout(TimeoutPolicyWait)
	defer cancel()
	if err := policywait.FileApplied(ctx, hostPath, want, policywait.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return &ExitError{Code: policywait.ExitCode(err)}
	}
	fmt.Println("policy set: wait ok (file settled)")
	return nil
}

// PolicyUpdate applies incremental network changes to the sandbox base policy.

// PolicyUpdate applies incremental network changes to the sandbox base policy.
func (a *App) PolicyUpdate(sandbox string, endpoints, allows, denies, binaries []string, wait bool) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()
	baseYAML, err := c.GetSandboxPolicy(ctx, sandbox, "base")
	if err != nil {
		return err
	}
	doc, err := policy.Parse(baseYAML)
	if err != nil {
		return fmt.Errorf("policy update: parse base: %w", err)
	}
	u := policy.NetworkUpdate{Binaries: binaries}
	for _, s := range endpoints {
		ep, err := policy.ParseEndpointSpec(s)
		if err != nil {
			return err
		}
		u.AddEndpoints = append(u.AddEndpoints, ep)
	}
	for _, s := range allows {
		mp, err := policy.ParseMethodPathSpec(s)
		if err != nil {
			return err
		}
		u.AddAllows = append(u.AddAllows, mp)
	}
	for _, s := range denies {
		mp, err := policy.ParseMethodPathSpec(s)
		if err != nil {
			return err
		}
		u.AddDenies = append(u.AddDenies, mp)
	}
	out, err := policy.ApplyNetworkUpdate(doc, u)
	if err != nil {
		return err
	}
	raw, err := yamlMarshalDoc(out)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "cautem-policy-update-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return a.PolicySet(sandbox, tmpPath, wait)
}

func yamlMarshalDoc(doc policy.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

// PolicyList prints policy revision metadata.

// PolicyList prints policy revision metadata.
func (a *App) PolicyList(sandbox string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	revs, err := c.ListPolicyRevisions(a.apiCtx(), sandbox)
	if err != nil {
		return err
	}
	if len(revs) == 0 {
		fmt.Println("policy list: (none)")
		return nil
	}
	for _, r := range revs {
		fmt.Printf("rev=%d status=%s bytes=%d at=%s\n", r.Rev, r.Status, r.Bytes, r.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

// PolicyGetRevision prints base YAML for a historical revision.

// PolicyGetRevision prints base YAML for a historical revision.
func (a *App) PolicyGetRevision(sandbox string, rev int) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	yamlBody, err := c.GetPolicyRevision(a.apiCtx(), sandbox, rev)
	if err != nil {
		return err
	}
	fmt.Print(yamlBody)
	if len(yamlBody) > 0 && yamlBody[len(yamlBody)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

// SandboxProviderList prints attached provider metadata (no secret values).

// SandboxProviderList prints attached provider metadata (no secret values).
func (a *App) SandboxProviderList(sandbox string) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	list, err := c.ListSandboxProviders(a.apiCtx(), sandbox)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("sandbox providers: (none)")
		return nil
	}
	for _, p := range list {
		fmt.Printf("%s\ttype=%s\tenv=%v\n", p.Name, p.Type, p.EnvVars)
	}
	return nil
}

// PolicyGet prints sandbox policy YAML. view is "base" or "full" (default full).

// PolicyGet prints sandbox policy YAML. view is "base" or "full" (default full).
func (a *App) PolicyGet(sandboxName, view string) error {
	if view == "" {
		view = "full"
	}
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	b, err := c.GetSandboxPolicy(a.apiCtx(), sandboxName, view)
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Println()
	}
	return nil
}

func writeFileInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func (a *App) mergeGatewayGlobal(doc policy.Document) (policy.Document, error) {
	u, err := a.currentGatewayURL()
	if err != nil || u == "" {
		return doc, nil
	}
	b, err := cautem.NewWithToken(u, a.gatewayTokenForURL(u)).GetGlobalPolicy(a.apiCtx())
	if err != nil || len(bytesTrim(b)) == 0 {
		return doc, nil
	}
	global, err := policy.Parse(b)
	if err != nil {
		return doc, fmt.Errorf("gateway global policy: %w", err)
	}
	return policy.MergeGlobal(doc, global)
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// InferenceList prints builtin provider presets.

func (a *App) loadOrDenyAll(path string) (policy.Document, string, error) {
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err := policy.Load(abs)
		if err != nil {
			return policy.Document{}, "", err
		}
		doc, err = a.mergeGatewayGlobal(doc)
		if err != nil {
			return policy.Document{}, "", err
		}
		if err := doc.Validate(); err != nil {
			return policy.Document{}, "", err
		}
		return doc, abs, nil
	}
	dir, err := os.MkdirTemp("", "cautem-policy-*")
	if err != nil {
		return policy.Document{}, "", err
	}
	abs := filepath.Join(dir, "deny-all.yaml")
	const body = "version: 1\nnetwork_policies: {}\n"
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		return policy.Document{}, "", err
	}
	doc, err := policy.Parse([]byte(body))
	if err != nil {
		return policy.Document{}, "", err
	}
	return doc, abs, nil
}

// InitOpts for `cautem init --agent …`.
