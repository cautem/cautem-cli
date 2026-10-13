package cli

import (
	"fmt"

	"github.com/cautem/cautem-cli/internal/service"
)

func runDoctor(a *service.App, args []string) error {
	// OpenShell: doctor check (doctor alone → check)
	if len(args) == 0 || args[0] == "check" {
		return a.Doctor()
	}
	if args[0] == "cleanup" {
		confirm := false
		explicitDryRun := false
		for _, arg := range args[1:] {
			switch arg {
			case "--yes", "-y":
				if explicitDryRun {
					return fmt.Errorf("usage: cautem doctor cleanup [--dry-run|--yes]")
				}
				confirm = true
			case "--dry-run":
				if confirm {
					return fmt.Errorf("usage: cautem doctor cleanup [--dry-run|--yes]")
				}
				explicitDryRun = true
			default:
				return fmt.Errorf("usage: cautem doctor cleanup [--dry-run|--yes]")
			}
		}
		return a.CleanupDockerTestResources(a.CommandContext(), confirm)
	}
	return fmt.Errorf("usage: cautem doctor check")
}
