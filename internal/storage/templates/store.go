// Package templates stores local sandbox workload templates (OpenShell sandbox template).
package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Template is a reusable sandbox create shape.
type Template struct {
	Name             string            `json:"name"`
	Image            string            `json:"image,omitempty"`
	From             string            `json:"from,omitempty"`
	Policy           string            `json:"policy,omitempty"`
	CPU              float64           `json:"cpu,omitempty"`
	Memory           string            `json:"memory,omitempty"` // e.g. 4Gi, 512m
	GPUCount         int               `json:"gpu_count,omitempty"`
	PidsLimit        int64             `json:"pids_limit,omitempty"`
	DriverConfigJSON string            `json:"driver_config_json,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	Providers        []string          `json:"providers,omitempty"`
	Forwards         []int             `json:"forwards,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

var validTemplateName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cautem", "templates"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cautem", "templates"), nil
}

func pathFor(name string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if !validTemplateName.MatchString(name) {
		return "", fmt.Errorf("invalid template name %q: use a lowercase DNS label up to 63 characters", name)
	}
	return filepath.Join(d, name+".json"), nil
}

// Save writes a template.
func Save(t Template) error {
	if t.Image != "" && t.From != "" {
		return fmt.Errorf("template: use either image or from, not both")
	}
	if t.CPU < 0 || t.GPUCount < 0 || t.PidsLimit < -1 {
		return fmt.Errorf("template: invalid CPU, GPU count, or pids limit")
	}
	p, err := pathFor(t.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(p), ".template-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(b); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, p)
}

// Get loads a template by name.
func Get(name string) (Template, error) {
	p, err := pathFor(name)
	if err != nil {
		return Template{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Template{}, err
	}
	var t Template
	if err := json.Unmarshal(b, &t); err != nil {
		return Template{}, err
	}
	t.Name = name
	return t, nil
}

// List returns all templates.
func List() ([]Template, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Template
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		t, err := Get(name)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Delete removes a template.
func Delete(name string) error {
	p, err := pathFor(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
