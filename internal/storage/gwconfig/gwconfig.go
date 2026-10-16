// Package gwconfig loads multi-gateway CLI config (~/.config/cautem/config.yaml).
package gwconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cautem/quarshell-cli/internal/securefile"
	"github.com/cautem/quarshell-core/defaults"
	"gopkg.in/yaml.v3"
)

// File is the on-disk CLI config.
type File struct {
	Current  string             `yaml:"current,omitempty"`
	Gateways map[string]Gateway `yaml:"gateways,omitempty"`
	// Images maps BYOC / community short names to container images (--from).
	Images map[string]string `yaml:"images,omitempty"`
	// Defaults apply on sandbox create when flags/template omit the field (non-breaking).
	Defaults CreateDefaults `yaml:"defaults,omitempty"`
}

// CreateDefaults are optional soft defaults for sandbox create (OpenShell template spirit).
type CreateDefaults struct {
	Memory    string  `yaml:"memory,omitempty"`     // e.g. 2g — only when --memory unset
	CPU       float64 `yaml:"cpu,omitempty"`        // only when --cpu unset
	PidsLimit int64   `yaml:"pids_limit,omitempty"` // only when --pids-limit unset; 0 skips
}

// Gateway is one named control-plane endpoint.
type Gateway struct {
	URL     string `yaml:"url"`
	DataDir string `yaml:"data_dir,omitempty"`
	Token   string `yaml:"token,omitempty"` // access token (local-dev or OIDC)

	// OIDC (OpenShell-compatible gateway add flags).
	OIDCIssuer       string `yaml:"oidc_issuer,omitempty"`
	OIDCClientID     string `yaml:"oidc_client_id,omitempty"`
	OIDCAudience     string `yaml:"oidc_audience,omitempty"`
	OIDCScopes       string `yaml:"oidc_scopes,omitempty"`
	OIDCAllowHTTP    bool   `yaml:"oidc_allow_insecure_http,omitempty"`
	RefreshToken     string `yaml:"refresh_token,omitempty"`
	TokenExpiresAtMS int64  `yaml:"token_expires_at_ms,omitempty"`
}

// Path returns the default config path.
func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cautem", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cautem", "config.yaml"), nil
}

// Load reads config or returns empty defaults.
func Load() (File, string, error) {
	p, err := Path()
	if err != nil {
		return File{}, "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return File{Gateways: map[string]Gateway{}}, p, nil
		}
		return File{}, p, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return File{}, p, fmt.Errorf("config: %w", err)
	}
	if f.Gateways == nil {
		f.Gateways = map[string]Gateway{}
	}
	if f.Images == nil {
		f.Images = map[string]string{}
	}
	return f, p, nil
}

// Save writes config atomically, owner-only: it holds gateway bearer and
// refresh tokens.
func Save(f File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(p, b)
}

// CurrentURL returns the selected gateway URL if any.
func CurrentURL(f File) string {
	if f.Current == "" {
		return ""
	}
	g, ok := f.Gateways[f.Current]
	if !ok {
		return ""
	}
	return g.URL
}

// ResolveImage expands configured/builtin --from aliases and otherwise treats
// the value as an OCI image reference.
func ResolveImage(from, explicit string) (string, error) {
	if explicit != "" && from != "" {
		return "", fmt.Errorf("use either --image or --from, not both")
	}
	if explicit != "" {
		return explicit, nil
	}
	if from == "" {
		return "", nil
	}
	cfg, _, err := Load()
	if err != nil {
		return "", err
	}
	if img, ok := cfg.Images[from]; ok && img != "" {
		return img, nil
	}
	builtins := map[string]string{
		"debian": defaults.ImageDebian,
		// cautem catalog shorts (local). GHCR paths when CAUTEM_USE_GHCR=1.
		"base":           defaults.ImageLocal,
		"cautem/cli":     defaults.ImageLocal,
		"cautem/gui":     defaults.ImageGUI,
		"cautem/gpu":     defaults.ImageGPU,
		"gui-gpu":        defaults.ImageGUIGPU,
		"cautem/gui-gpu": defaults.ImageGUIGPU,
		"cursor":         defaults.ImageCursor,
		"claude":         defaults.ImageClaude,
		"codex":          defaults.ImageCodex,
		// Opt-in NVIDIA OpenShell community interop.
		"community/base":   "ghcr.io/nvidia/openshell-community/sandboxes/base:latest",
		"community/ollama": "ghcr.io/nvidia/openshell-community/sandboxes/ollama:latest",
		"ollama":           "ghcr.io/nvidia/openshell-community/sandboxes/ollama:latest",
	}
	// After images:pull / CI publish, set images.* in config or CAUTEM_USE_GHCR=1 for GHCR builtins.
	if os.Getenv("CAUTEM_USE_GHCR") == "1" {
		builtins["base"] = defaults.ImageBaseRef
		builtins["cautem/cli"] = defaults.ImageBaseRef
		builtins["cautem/gui"] = defaults.ImageGUIRef
		builtins["cautem/gpu"] = defaults.ImageGPURef
		builtins["gui-gpu"] = defaults.ImageGUIGPURef
		builtins["cautem/gui-gpu"] = defaults.ImageGUIGPURef
		builtins["cursor"] = defaults.ImageCursorRef
		builtins["claude"] = defaults.ImageClaudeRef
		builtins["codex"] = defaults.ImageCodexRef
	}
	if img, ok := builtins[from]; ok {
		return img, nil
	}
	return from, nil
}
