package cli

import (
	"fmt"

	"github.com/cautem/quarshell-cli/internal/service"
)

func runInstall(a *service.App, args []string) error {
	opt := service.InstallOpts{}
	for i := range args {
		switch args[i] {
		case "--force":
			opt.Force = true
		default:
			return fmt.Errorf("unknown flag %q (usage: cautem install [--force])", args[i])
		}
	}
	return a.Install(opt)
}
