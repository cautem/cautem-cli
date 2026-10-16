package service

import (
	"fmt"
	"os"

	"github.com/cautem/quarshell-cli/internal/osargs"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

// InferenceRouteGet prints the gateway inference route.
func (a *App) InferenceRouteGet() error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	route, err := c.GetInference(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("provider: %s\nmodel: %s\n", route.Provider, route.Model)
	if route.TimeoutSec > 0 {
		fmt.Printf("timeout_sec: %d\n", route.TimeoutSec)
	}
	if route.Version > 0 {
		fmt.Printf("version: %d\n", route.Version)
	}
	return nil
}

// InferenceRouteSet applies gateway inference routing (OpenShell inference set).
func (a *App) InferenceRouteSet(opt osargs.InferenceSet) error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()
	route := cautem.InferenceRoute{
		Provider:   opt.Provider,
		Model:      opt.Model,
		TimeoutSec: opt.TimeoutSec,
	}
	if !opt.NoVerify {
		if _, err := c.GetProvider(ctx, opt.Provider); err != nil {
			return fmt.Errorf("inference set: provider %q: %w (use --no-verify to skip)", opt.Provider, err)
		}
	}
	out, err := c.PutInference(ctx, route)
	if err != nil {
		return err
	}
	fmt.Printf("inference route set: provider=%s model=%s timeout_sec=%d version=%d\n",
		out.Provider, out.Model, out.TimeoutSec, out.Version)
	return nil
}

// InferenceRouteDelete clears gateway inference routing.
func (a *App) InferenceRouteDelete() error {
	c, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	if err := c.DeleteInference(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "inference route deleted")
	return nil
}
