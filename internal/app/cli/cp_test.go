package cli

import (
	"strings"
	"testing"

	"github.com/cautem/cautem-cli/internal/service"
)

func TestCopyCommandRequiresTwoPaths(t *testing.T) {
	for _, args := range [][]string{{"cp"}, {"cp", "source"}, {"cp", "source", "dest", "extra"}} {
		err := execute(&service.App{}, args)
		if err == nil || !strings.Contains(err.Error(), "usage: cautem cp") {
			t.Errorf("execute(%v) error=%v; want cp usage", args, err)
		}
	}
}

func TestCopyHelpDocumentsBothDirections(t *testing.T) {
	help := helpText([]string{"cp"})
	for _, want := range []string{"cautem cp <local-path> <sandbox>:/path", "cautem cp <sandbox>:/path <local-path>"} {
		if !strings.Contains(help, want) {
			t.Errorf("cp help missing %q", want)
		}
	}
}
