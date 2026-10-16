// Package sandboxstate stores per-gateway CLI sandbox selection state.
package sandboxstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/cautem/quarshell-cli/internal/securefile"
	"gopkg.in/yaml.v3"
)

type stateFile struct {
	Last map[string]string `yaml:"last,omitempty"`
}

// SetLast records the most recently selected sandbox for a gateway/workspace.
func SetLast(gateway, workspace, name string) error {
	path, err := path()
	if err != nil {
		return err
	}
	state, err := load(path)
	if err != nil {
		return err
	}
	state.Last[key(gateway, workspace)] = name
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return securefile.WriteAtomic(path, data)
}

// GetLast returns the last sandbox for a gateway/workspace, if one is stored.
func GetLast(gateway, workspace string) (string, error) {
	path, err := path()
	if err != nil {
		return "", err
	}
	state, err := load(path)
	if err != nil {
		return "", err
	}
	return state.Last[key(gateway, workspace)], nil
}

func path() (string, error) {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "cautem", "last-sandbox.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cautem", "last-sandbox.yaml"), nil
}

func load(path string) (stateFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return stateFile{Last: map[string]string{}}, nil
	}
	if err != nil {
		return stateFile{}, err
	}
	var state stateFile
	if err := yaml.Unmarshal(data, &state); err != nil {
		return stateFile{}, err
	}
	if state.Last == nil {
		state.Last = map[string]string{}
	}
	return state, nil
}

func key(gateway, workspace string) string {
	sum := sha256.Sum256([]byte(gateway + "\x00" + workspace))
	return hex.EncodeToString(sum[:])
}
