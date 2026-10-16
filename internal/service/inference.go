package service

import (
	"fmt"
	"os"

	"github.com/cautem/quarshell-core/policy"
	"github.com/cautem/quarshell-runtime/inference"
)

// InferenceList prints builtin provider presets.
func (a *App) InferenceList() error {
	return inference.ListBuiltins(os.Stdout)
}

// InferenceShow prints effective inference + network allow rules for a policy file.

// InferenceShow prints effective inference + network allow rules for a policy file.
func (a *App) InferenceShow(path string) error {
	if path == "" {
		path = "policies/default.yaml"
	}
	doc, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("inference show: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("inference show: %w", err)
	}
	return inference.ShowEffective(os.Stdout, doc)
}

// InferenceLocal prints a host-local model policy snippet.

// InferenceLocal prints a host-local model policy snippet.
func (a *App) InferenceLocal() error {
	return inference.WriteLocalSnippet(os.Stdout)
}

// SandboxCreateOpts are CLI flags for sandbox create.
