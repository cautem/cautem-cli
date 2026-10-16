package service

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/cautem/cautem-driver/driver"
)

// ParseCPUQuantity accepts OpenShell-style CPU quantities expressed as cores
// (for example 0.5 or 2) or millicores (for example 500m).
func ParseCPUQuantity(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("CPU quantity is empty")
	}
	multiplier := 1.0
	if strings.HasSuffix(value, "m") {
		multiplier = 0.001
		value = strings.TrimSuffix(value, "m")
	}
	cores, err := strconv.ParseFloat(value, 64)
	if err != nil || cores <= 0 || math.IsNaN(cores) || math.IsInf(cores, 0) {
		return 0, fmt.Errorf("invalid CPU quantity %q", raw)
	}
	cores *= multiplier
	if cores <= 0 || math.IsInf(cores, 0) {
		return 0, fmt.Errorf("invalid CPU quantity %q", raw)
	}
	return cores, nil
}

func validateSandboxCreateResources(opt SandboxCreateOpts) error {
	if opt.CPU < 0 || math.IsNaN(opt.CPU) || math.IsInf(opt.CPU, 0) {
		return fmt.Errorf("sandbox create: CPU must be a positive finite quantity")
	}
	if opt.Memory != "" {
		if _, err := ParseMemoryQuantity(opt.Memory); err != nil {
			return fmt.Errorf("sandbox create --memory: %w", err)
		}
	}
	if opt.PidsLimit < -1 {
		return fmt.Errorf("sandbox create: --pids-limit must be -1 or a positive integer")
	}
	if err := validateDriverConfigJSON(opt.DriverConfigJSON); err != nil {
		return fmt.Errorf("sandbox create --driver-config-json: %w", err)
	}
	if opt.GPUCount < 0 || uint64(opt.GPUCount) > uint64(^uint32(0)) {
		return fmt.Errorf("sandbox create: GPU count must be a positive 32-bit integer")
	}
	if opt.GPUCount > 0 && !opt.GPU {
		return fmt.Errorf("sandbox create: GPU count requires --gpu")
	}
	if len(opt.CDIDevices) > 0 {
		if !opt.GPU && opt.GPUCount == 0 && !envTruthy("CAUTEM_GPU") {
			return fmt.Errorf("sandbox create: --cdi requires --gpu")
		}
		if opt.GPUCount > 0 && len(opt.CDIDevices) != opt.GPUCount {
			return fmt.Errorf("sandbox create: --gpu count %d does not match %d explicit CDI devices", opt.GPUCount, len(opt.CDIDevices))
		}
		seen := make(map[string]struct{}, len(opt.CDIDevices))
		for _, device := range opt.CDIDevices {
			device = strings.TrimSpace(device)
			if device == "" {
				return fmt.Errorf("sandbox create: CDI device ID cannot be empty")
			}
			if _, exists := seen[device]; exists {
				return fmt.Errorf("sandbox create: duplicate CDI device ID %q", device)
			}
			seen[device] = struct{}{}
		}
	}
	return nil
}

func validateDriverConfigJSON(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("invalid JSON object: %w", err)
	}
	if len(config) == 0 {
		return fmt.Errorf("expected a non-empty object keyed by driver name")
	}
	for name, value := range config {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("driver name cannot be empty")
		}
		var block map[string]json.RawMessage
		if err := json.Unmarshal(value, &block); err != nil || block == nil {
			return fmt.Errorf("driver %q config must be an object", name)
		}
	}
	return nil
}

// ParseMemoryQuantity accepts Docker-style quantities plus Kubernetes binary
// byte quantities such as 512Mi and 4Gi.
func ParseMemoryQuantity(raw string) (int64, error) {
	if bytes, err := driver.ParseMemoryBytes(raw); err == nil && bytes > 0 {
		return bytes, nil
	}
	value := strings.TrimSpace(raw)
	for _, unit := range []struct {
		suffix string
		power  int64
	}{{"Ei", 6}, {"Pi", 5}, {"Ti", 4}, {"Gi", 3}, {"Mi", 2}, {"Ki", 1}} {
		if !strings.HasSuffix(value, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
		quantity, ok := new(big.Rat).SetString(number)
		if !ok || quantity.Sign() <= 0 {
			break
		}
		multiplier := new(big.Int).Exp(big.NewInt(1024), big.NewInt(unit.power), nil)
		quantity.Mul(quantity, new(big.Rat).SetInt(multiplier))
		if !quantity.IsInt() || !quantity.Num().IsInt64() {
			break
		}
		return quantity.Num().Int64(), nil
	}
	return 0, fmt.Errorf("invalid memory quantity %q", raw)
}

func validateTemplateCreateConflict(opt SandboxCreateOpts) error {
	if opt.Template == "" {
		return nil
	}
	if opt.Image != "" || opt.From != "" || opt.BuildContext != "" || opt.Dockerfile != "" || opt.CPU != 0 || opt.Memory != "" || opt.GPU || opt.GPUCount != 0 ||
		len(opt.CDIDevices) > 0 || opt.Env != nil || opt.DriverConfigJSON != "" || opt.PidsLimit != 0 {
		return fmt.Errorf("sandbox create: --template cannot be combined with workload options --from, --image, --build, --dockerfile, --cpu, --memory, --gpu, --cdi, --env, --pids-limit, or --driver-config-json")
	}
	return nil
}

func applyCreateResources(spec *driver.Spec, opt SandboxCreateOpts, memoryBytes int64) {
	spec.GPU = opt.GPU || envTruthy("CAUTEM_GPU")
	spec.GPUCount = opt.GPUCount
	spec.CDIDevices = append([]string(nil), opt.CDIDevices...)
	spec.CPU = opt.CPU
	spec.MemoryBytes = memoryBytes
	spec.PidsLimit = opt.PidsLimit
}
