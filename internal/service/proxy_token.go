package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sandboxTokenFromEnvironment() (string, error) {
	if token := strings.TrimSpace(os.Getenv(EnvSandboxToken)); token != "" {
		return token, nil
	}

	path := strings.TrimSpace(os.Getenv(EnvSandboxTokenFile))
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("proxy: %s must be a clean absolute path", EnvSandboxTokenFile)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("proxy: read sandbox token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("proxy: sandbox token file is empty")
	}
	return token, nil
}
