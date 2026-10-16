// SPDX-FileCopyrightText: Copyright (c) 2026 cautem
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"fmt"
	"os"

	core "github.com/cautem/quarshell-core"
	"github.com/cautem/quarshell-driver/driver"
	"github.com/cautem/quarshell-runtime/agentconfig"
)

type guestDriver interface {
	Exec(context.Context, core.ID, driver.ExecRequest) (driver.ExecResult, error)
	CopyTo(context.Context, core.ID, string, string) error
}

type sandboxGuest struct {
	drv guestDriver
	id  core.ID
	ctx context.Context
}

func (g sandboxGuest) ExecRaw(argv []string) error {
	res, err := g.drv.Exec(g.ctx, g.id, driver.ExecRequest{Argv: argv, WorkDir: "/"})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit %d", res.ExitCode)
	}
	return nil
}

func (g sandboxGuest) CopyTo(hostSrc, guestDest string) error {
	return g.drv.CopyTo(g.ctx, g.id, hostSrc, guestDest)
}

// installAgentConfig installs only the supervisor-owned policy advisor and
// no-overwrite AGENTS.md pointer. Agent configuration remains image-owned.
func (a *App) installAgentConfig(h driver.Handle) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox guidance: compute driver unavailable")
	}
	st, err := agentconfig.Stage(agentconfig.DefaultOptions())
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(st.Dir) }()
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	return agentconfig.Install(sandboxGuest{drv: a.Sandboxes.Driver, id: h.ID, ctx: ctx}, st)
}
