package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitOpts for `cautem init --agent …`.
type InitOpts struct {
	Agent string
	Dir   string
	Force bool
}

// Init writes a starter policy for a known agent.

// Init writes a starter policy for a known agent.
func (a *App) Init(opt InitOpts) error {
	agent := strings.ToLower(strings.TrimSpace(opt.Agent))
	dir := opt.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var srcName string
	switch agent {
	case "cursor":
		srcName = "cursor.yaml"
	default:
		return fmt.Errorf("init: unknown agent %q (supported: cursor)", opt.Agent)
	}
	mod, err := findModuleDir("github.com/cautem/cautem-cli")
	if err != nil {
		return err
	}
	src := filepath.Join(mod, "policies", srcName)
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("init: read bundled policy: %w", err)
	}
	outDir := filepath.Join(absDir, "policies")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(outDir, srcName)
	if _, err := os.Stat(dst); err == nil && !opt.Force {
		return fmt.Errorf("init: %s already exists (use --force)", dst)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("init: wrote %s\n", dst)
	fmt.Printf("next: cautem policy check %s\n", dst)
	fmt.Printf("      cautem agent login %s\n", agent)
	return nil
}

// AgentLogin checks host env keys for a named agent (MVP: no OAuth).

// AgentLogin checks host env keys for a named agent (MVP: no OAuth).
func (a *App) AgentLogin(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cursor":
		keys := []string{"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"}
		fmt.Println("agent login cursor: checking host env (no tokens printed)")
		found := 0
		for _, k := range keys {
			if os.Getenv(k) != "" {
				fmt.Printf("  %s: set\n", k)
				found++
			} else {
				fmt.Printf("  %s: missing\n", k)
			}
		}
		if found == 0 {
			return fmt.Errorf("agent login: set at least one of %s", strings.Join(keys, ", "))
		}
		fmt.Println("ok: use --provider cursor (and --provider github if needed) on sandbox create")
		fmt.Println("docs: https://cautem.github.io/sandbox.dev/guides/cursor/")
		return nil
	default:
		return fmt.Errorf("agent login: unknown agent %q (supported: cursor)", name)
	}
}
