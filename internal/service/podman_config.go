package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const maxDriverConfigBytes = 1 << 20

// podmanDriverConfigFromEnvironment adapts legacy Gateway TOML and native
// JSON into one driver map. Native values override legacy values per key;
// legacy Gateway defaults are inherited before that overlay is applied.
func podmanDriverConfigFromEnvironment() (map[string]any, error) {
	out := map[string]any{}
	path := os.Getenv("OPENSHELL_CONFIG")
	if path != "" {
		document, err := readGatewayConfig(path)
		if err != nil {
			return nil, err
		}
		root, _ := document["openshell"].(map[string]any)
		drivers, _ := root["drivers"].(map[string]any)
		config, _ := drivers["podman"].(map[string]any)
		for key, value := range config {
			out[key] = value
		}
		// Keep the pinned Podman registration's inheritance list. Unsupported
		// runtime fields must reach ConfigFromMap and fail closed, not disappear.
		gateway, _ := root["gateway"].(map[string]any)
		for _, key := range []string{"default_image", "supervisor_image", "host_gateway_ip", "guest_tls_ca", "guest_tls_cert", "guest_tls_key"} {
			if _, exists := out[key]; !exists {
				if value, exists := gateway[key]; exists {
					out[key] = value
				}
			}
		}
	}
	if raw := os.Getenv("CAUTEM_PODMAN_CONFIG"); raw != "" {
		if len(raw) > maxDriverConfigBytes {
			return nil, fmt.Errorf("CAUTEM_PODMAN_CONFIG exceeds %d bytes", maxDriverConfigBytes)
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(raw), &config); err != nil || config == nil {
			return nil, fmt.Errorf("CAUTEM_PODMAN_CONFIG must be a JSON object")
		}
		for key, value := range config {
			out[key] = value
		}
	}
	return out, nil
}

func readGatewayConfig(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read Gateway config: %w", err)
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxDriverConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Gateway config: %w", err)
	}
	if len(body) > maxDriverConfigBytes {
		return nil, fmt.Errorf("gateway config exceeds %d bytes", maxDriverConfigBytes)
	}
	var document map[string]any
	if err := toml.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("parse Gateway config: %w", err)
	}
	return document, nil
}

// configuredComputeDriversFromEnvironment reads the enabled compute registry
// from the same legacy Gateway TOML used for driver-specific settings.
func configuredComputeDriversFromEnvironment() ([]string, error) {
	path := os.Getenv("OPENSHELL_CONFIG")
	if path == "" {
		return nil, nil
	}
	document, err := readGatewayConfig(path)
	if err != nil {
		return nil, err
	}
	root, _ := document["openshell"].(map[string]any)
	gateway, _ := root["gateway"].(map[string]any)
	raw, exists := gateway["compute_drivers"]
	if !exists {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("gateway.compute_drivers must be a non-empty string array")
	}
	drivers := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		name, ok := value.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("gateway.compute_drivers entries must be non-empty strings")
		}
		name = normalizeSelectedDriver(name)
		if name != "docker" && name != "podman" && name != "vm" && name != "kubernetes" {
			return nil, fmt.Errorf("unknown compute driver %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate compute driver %q", name)
		}
		seen[name] = true
		drivers = append(drivers, name)
	}
	return drivers, nil
}
