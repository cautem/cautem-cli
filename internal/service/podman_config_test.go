package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPodmanDriverConfigReadsCompatTablesAndSharedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	data := []byte("[openshell.gateway]\ndefault_image = \"shared:image\"\nsupervisor_image = \"shared:supervisor\"\nhost_gateway_ip = \"10.0.0.2\"\nguest_tls_ca = \"/certs/ca.pem\"\nguest_tls_cert = \"/certs/client.pem\"\nguest_tls_key = \"/certs/client.key\"\n[openshell.drivers.podman]\nnetwork_name = \"sandbox-net\"\nstop_timeout_secs = 15\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHELL_CONFIG", path)
	t.Setenv("CAUTEM_PODMAN_CONFIG", "")
	config, err := podmanDriverConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config["default_image"] != "shared:image" || config["supervisor_image"] != "shared:supervisor" || config["host_gateway_ip"] != "10.0.0.2" || config["guest_tls_ca"] != "/certs/ca.pem" || config["guest_tls_cert"] != "/certs/client.pem" || config["guest_tls_key"] != "/certs/client.key" || config["network_name"] != "sandbox-net" || config["stop_timeout_secs"] != int64(15) {
		t.Fatalf("driver config=%#v", config)
	}
}

func TestPodmanDriverConfigMergesNativeOverridesOverCompatAndGatewayDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	data := []byte("[openshell.gateway]\ndefault_image = \"shared:image\"\nhost_gateway_ip = \"10.0.0.2\"\n[openshell.drivers.podman]\ndefault_image = \"driver:image\"\nnetwork_name = \"openshell-net\"\nstop_timeout_secs = 15\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHELL_CONFIG", path)
	t.Setenv("CAUTEM_PODMAN_CONFIG", `{"network_name":"native-net","enable_bind_mounts":true}`)

	config, err := podmanDriverConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config["network_name"] != "native-net" || config["default_image"] != "driver:image" || config["host_gateway_ip"] != "10.0.0.2" || config["stop_timeout_secs"] != int64(15) || config["enable_bind_mounts"] != true {
		t.Fatalf("merged driver config=%#v", config)
	}
}

func TestPodmanDriverConfigRejectsNonObjectAndOversizedNativeConfig(t *testing.T) {
	for _, raw := range []string{"null", "[]", strings.Repeat("x", maxDriverConfigBytes+1)} {
		t.Setenv("OPENSHELL_CONFIG", "")
		t.Setenv("CAUTEM_PODMAN_CONFIG", raw)
		if _, err := podmanDriverConfigFromEnvironment(); err == nil {
			t.Fatalf("accepted invalid native driver config of length %d", len(raw))
		}
	}
}

func TestPodmanDriverConfigBoundsCompatConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte(strings.Repeat("#", maxDriverConfigBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHELL_CONFIG", path)
	t.Setenv("CAUTEM_PODMAN_CONFIG", "")
	if _, err := podmanDriverConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized OpenShell config error=%v", err)
	}
}

func TestSelectedDriverUsesCompatComputeDriverSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[openshell.gateway]\ncompute_drivers = [\"podman\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHELL_CONFIG", path)
	t.Setenv("CAUTEM_DRIVER", "")
	if got, err := selectedDriver(); err != nil || got != "podman" {
		t.Fatalf("selectedDriver()=(%q, %v), want podman", got, err)
	}
	t.Setenv("CAUTEM_DRIVER", "docker")
	if _, err := selectedDriver(); err == nil {
		t.Fatal("explicit CAUTEM_DRIVER outside compute_drivers was accepted")
	}
}

func TestSelectedDriverKeepsDockerDefaultForMultiDriverCompatConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[openshell.gateway]\ncompute_drivers = [\"podman\", \"docker\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHELL_CONFIG", path)
	t.Setenv("CAUTEM_DRIVER", "")
	if got, err := selectedDriver(); err != nil || got != "docker" {
		t.Fatalf("selectedDriver()=(%q, %v), want docker", got, err)
	}
}

func TestSelectedDriverRejectsDuplicateOrEmptyCompatSelection(t *testing.T) {
	for _, input := range []string{
		"[openshell.gateway]\ncompute_drivers = []\n",
		"[openshell.gateway]\ncompute_drivers = [\"podman\", \"podman\"]\n",
	} {
		path := filepath.Join(t.TempDir(), "gateway.toml")
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("OPENSHELL_CONFIG", path)
		t.Setenv("CAUTEM_DRIVER", "")
		if _, err := selectedDriver(); err == nil {
			t.Fatalf("selectedDriver accepted invalid config %q", input)
		}
	}
}
