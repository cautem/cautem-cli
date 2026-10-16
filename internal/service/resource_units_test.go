package service

import (
	"math"
	"testing"

	"github.com/cautem/quarshell-driver/driver"
)

func TestParseCPUQuantity(t *testing.T) {
	for input, want := range map[string]float64{"500m": 0.5, "2": 2, "2.5": 2.5} {
		got, err := ParseCPUQuantity(input)
		if err != nil || math.Abs(got-want) > 1e-9 {
			t.Errorf("ParseCPUQuantity(%q)=(%v,%v), want %v", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0", "-1", "1cpu", "NaN", "+Inf", "0m"} {
		if _, err := ParseCPUQuantity(input); err == nil {
			t.Errorf("ParseCPUQuantity(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseMemoryQuantity(t *testing.T) {
	for input, want := range map[string]int64{"4Gi": 4 << 30, "512Mi": 512 << 20, "1.5Gi": 3 << 29, "2g": 2 << 30} {
		got, err := ParseMemoryQuantity(input)
		if err != nil || got != want {
			t.Errorf("ParseMemoryQuantity(%q)=(%d,%v), want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", "0Gi", "-1Gi", "invalid", "999999999999Ei"} {
		if _, err := ParseMemoryQuantity(input); err == nil {
			t.Errorf("ParseMemoryQuantity(%q) unexpectedly succeeded", input)
		}
	}
}

func TestValidateTemplateCreateConflictAndResources(t *testing.T) {
	if err := validateTemplateCreateConflict(SandboxCreateOpts{Template: "gpu", CPU: 1}); err == nil {
		t.Fatal("template combined with inline CPU was accepted")
	}
	if err := validateTemplateCreateConflict(SandboxCreateOpts{Template: "gpu", Policy: "policy.yaml", Labels: map[string]string{"team": "agents"}}); err != nil {
		t.Fatalf("create-time policy/labels should remain allowed: %v", err)
	}
	for _, opts := range []SandboxCreateOpts{
		{CPU: math.Inf(1)}, {Memory: "invalid"}, {PidsLimit: -2}, {GPUCount: 2},
		{CDIDevices: []string{"gpu0"}}, {GPU: true, GPUCount: 2, CDIDevices: []string{"gpu0"}},
		{GPU: true, CDIDevices: []string{"gpu0", "gpu0"}}, {DriverConfigJSON: `[]`},
	} {
		if err := validateSandboxCreateResources(opts); err == nil {
			t.Errorf("invalid resources %+v were accepted", opts)
		}
	}
}

func TestApplyCreateResourcesPreservesCompatResourceIntent(t *testing.T) {
	computeSpec := driver.Spec{}
	applyCreateResources(&computeSpec, SandboxCreateOpts{
		GPU: true, GPUCount: 2, CDIDevices: []string{"nvidia.com/gpu=0", "nvidia.com/gpu=1"}, CPU: 0.5, PidsLimit: 1024,
	}, 4<<30)
	if !computeSpec.GPU || computeSpec.GPUCount != 2 || computeSpec.CPU != 0.5 || computeSpec.MemoryBytes != 4<<30 || computeSpec.PidsLimit != 1024 || len(computeSpec.CDIDevices) != 2 {
		t.Fatalf("driver resource spec = %+v", computeSpec)
	}
}
