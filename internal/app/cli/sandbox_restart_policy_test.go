package cli

import (
	"strings"
	"testing"

	"github.com/cautem/cautem-cli/internal/service"
)

func TestSandboxCreateRestartPolicyFlag(t *testing.T) {
	for _, value := range []string{"never", "on-failure", "always"} {
		err := runSandbox(&service.App{}, []string{"create", "--restart-policy", value})
		if err == nil || !strings.Contains(err.Error(), "docker not available") {
			t.Errorf("valid restart policy %q was not parsed; error=%v", value, err)
		}
	}
	for _, args := range [][]string{
		{"create", "--restart-policy"},
		{"create", "--restart-policy", "sometimes"},
	} {
		if err := runSandbox(&service.App{}, args); err == nil || !strings.Contains(err.Error(), "restart-policy") {
			t.Errorf("invalid restart-policy args %v error=%v; want a flag validation error", args, err)
		}
	}
}
