// SPDX-FileCopyrightText: Copyright (c) 2026 cautem
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
	"github.com/cautem/quarshell-cli/internal/storage/templates"
)

func TestApplyCreateDefaultsFromEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envDefaultMemory, "2g")
	t.Setenv(envDefaultCPU, "1.5")
	t.Setenv(envDefaultPidsLimit, "1024")
	opt := &SandboxCreateOpts{}
	applyCreateDefaults(opt)
	if opt.Memory != "2g" || opt.CPU != 1.5 || opt.PidsLimit != 1024 {
		t.Fatalf("opt=%+v", opt)
	}
}

func TestApplyCreateDefaultsConfigOverEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(envDefaultMemory, "8g")
	cfg := gwconfig.File{
		Defaults: gwconfig.CreateDefaults{Memory: "3g", CPU: 2, PidsLimit: -1},
	}
	if err := gwconfig.Save(cfg); err != nil {
		t.Fatal(err)
	}
	opt := &SandboxCreateOpts{}
	applyCreateDefaults(opt)
	if opt.Memory != "3g" || opt.CPU != 2 || opt.PidsLimit != -1 {
		t.Fatalf("config should win over env: %+v", opt)
	}
}

func TestApplyCreateDefaultsDoesNotOverrideFlags(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(envDefaultMemory, "2g")
	opt := &SandboxCreateOpts{Memory: "4g", CPU: 3, PidsLimit: 4096}
	applyCreateDefaults(opt)
	if opt.Memory != "4g" || opt.CPU != 3 || opt.PidsLimit != 4096 {
		t.Fatalf("flags overridden: %+v", opt)
	}
}

func TestMergeTemplateIntoCreate(t *testing.T) {
	opt := &SandboxCreateOpts{}
	mergeTemplateIntoCreate(opt, templates.Template{
		Memory: "4Gi", CPU: 2, GPUCount: 2, PidsLimit: 2048, From: "cursor", DriverConfigJSON: `{"docker":{}}`,
	})
	if opt.Memory != "4Gi" || opt.CPU != 2 || !opt.GPU || opt.GPUCount != 2 || opt.PidsLimit != 2048 || opt.From != "cursor" || opt.DriverConfigJSON == "" {
		t.Fatalf("%+v", opt)
	}
	opt2 := &SandboxCreateOpts{Memory: "1g"}
	mergeTemplateIntoCreate(opt2, templates.Template{Memory: "4Gi"})
	if opt2.Memory != "1g" {
		t.Fatal("CLI memory must win over template")
	}
}

func TestTemplateRoundTripPids(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.MkdirAll(filepath.Join(dir, "cautem", "templates"), 0o755)
	t.Run("save", func(t *testing.T) {
		if err := templates.Save(templates.Template{Name: "desk", Memory: "2g", PidsLimit: 2048}); err != nil {
			t.Fatal(err)
		}
		got, err := templates.Get("desk")
		if err != nil {
			t.Fatal(err)
		}
		if got.Memory != "2g" || got.PidsLimit != 2048 {
			t.Fatalf("%+v", got)
		}
	})
}
