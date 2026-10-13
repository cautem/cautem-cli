package service

import (
	"fmt"

	"github.com/cautem/cautem-cli/internal/osargs"
	"github.com/cautem/cautem-cli/internal/storage/gwconfig"
)

// GatewayAdd registers a named gateway in config.
func (a *App) GatewayAdd(name, url string) error {
	return a.GatewayAddParsed(osargs.GatewayAdd{Name: name, Endpoint: url})
}

// GatewayAddParsed registers a gateway including optional OIDC metadata.

// GatewayAddParsed registers a gateway including optional OIDC metadata.
func (a *App) GatewayAddParsed(parsed osargs.GatewayAdd) error {
	name, url := parsed.Name, parsed.Endpoint
	if name == "" || url == "" {
		return fmt.Errorf("usage: cautem gateway add <endpoint> [--name NAME] [--local] [--oidc-issuer URL]")
	}
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	g := gwconfig.Gateway{
		URL:           url,
		OIDCIssuer:    parsed.OIDCIssuer,
		OIDCClientID:  parsed.OIDCClientID,
		OIDCAudience:  parsed.OIDCAudience,
		OIDCScopes:    parsed.OIDCScopes,
		OIDCAllowHTTP: parsed.OIDCAllowHTTP,
	}
	// Preserve tokens if re-adding same name with same URL.
	if prev, ok := cfg.Gateways[name]; ok && prev.URL == url {
		g.Token = prev.Token
		g.RefreshToken = prev.RefreshToken
		g.TokenExpiresAtMS = prev.TokenExpiresAtMS
		if g.OIDCIssuer == "" {
			g.OIDCIssuer = prev.OIDCIssuer
		}
		if g.OIDCClientID == "" {
			g.OIDCClientID = prev.OIDCClientID
		}
	}
	cfg.Gateways[name] = g
	cfg.Current = name
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("gateway add: ok name=%s url=%s config=%s\n", name, url, path)
	if g.OIDCIssuer != "" {
		fmt.Printf("gateway add: oidc issuer=%s client_id=%s\n", g.OIDCIssuer, g.OIDCClientID)
		fmt.Printf("gateway add: run `cautem gateway login` for Authorization Code + PKCE\n")
	}
	return nil
}

// GatewaySelect sets the current gateway.

// GatewaySelect sets the current gateway.
func (a *App) GatewaySelect(name string) error {
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	if _, ok := cfg.Gateways[name]; !ok {
		return fmt.Errorf("gateway select: unknown %q", name)
	}
	cfg.Current = name
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("gateway select: %s (%s)\n", name, path)
	return nil
}

// GatewayStatus prints local config + remote /healthz when reachable.

// GatewayStatus prints local config + remote /healthz when reachable.
func (a *App) GatewayStatus() error {
	cfg, path, err := gwconfig.Load()
	if err != nil {
		return err
	}
	fmt.Printf("config: %s\n", path)
	fmt.Printf("current: %s\n", cfg.Current)
	for name, g := range cfg.Gateways {
		mark := " "
		if name == cfg.Current {
			mark = "*"
		}
		fmt.Printf("%s %s  %s\n", mark, name, g.URL)
	}
	if u := gwconfig.CurrentURL(cfg); u != "" {
		ctx, cancel := a.withTimeout(TimeoutAPIShort)
		defer cancel()
		info, err := a.clientFor(u).Healthz(ctx)
		if err != nil {
			fmt.Printf("healthz: unreachable (%v)\n", err)
			return nil
		}
		fmt.Printf("healthz: ok %+v\n", info)
	}
	return nil
}

// GatewayListRemote lists sandboxes registered on the current gateway.

// GatewayListRemote lists sandboxes registered on the current gateway.
func (a *App) GatewayListRemote() error {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return err
	}
	u := gwconfig.CurrentURL(cfg)
	if u == "" {
		return fmt.Errorf("gateway list: no current gateway (cautem gateway add …)")
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	list, err := a.clientFor(u).ListSandboxes(ctx)
	if err != nil {
		return err
	}
	for _, sb := range list {
		fmt.Printf("%s\tid=%s\tstatus=%s\timage=%s\n", sb.Name, shortID(sb.ID), sb.Status, sb.Image)
	}
	if len(list) == 0 {
		fmt.Println("(no sandboxes registered)")
	}
	return nil
}

// ExecOpts for cautem sandbox exec.
