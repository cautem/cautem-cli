// SPDX-FileCopyrightText: Copyright (c) 2026 cautem
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"os"
	"strconv"
	"strings"

	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
)

const (
	envDefaultMemory    = "CAUTEM_DEFAULT_MEMORY"
	envDefaultCPU       = "CAUTEM_DEFAULT_CPU"
	envDefaultPidsLimit = "CAUTEM_DEFAULT_PIDS_LIMIT"
)

// applyCreateDefaults fills Memory/CPU/PidsLimit from config.yaml defaults then
// env, only when still unset. Does not force a hard-coded memory (OpenShell parity:
// uncapped unless operator opts in via template / config / env / flag).
func applyCreateDefaults(opt *SandboxCreateOpts) {
	if opt == nil {
		return
	}
	cfg, _, err := gwconfig.Load()
	if err == nil {
		if opt.Memory == "" && cfg.Defaults.Memory != "" {
			opt.Memory = cfg.Defaults.Memory
		}
		if opt.CPU == 0 && cfg.Defaults.CPU > 0 {
			opt.CPU = cfg.Defaults.CPU
		}
		if opt.PidsLimit == 0 && cfg.Defaults.PidsLimit != 0 {
			opt.PidsLimit = cfg.Defaults.PidsLimit
		}
	}
	if opt.Memory == "" {
		if v := strings.TrimSpace(os.Getenv(envDefaultMemory)); v != "" {
			opt.Memory = v
		}
	}
	if opt.CPU == 0 {
		if v := strings.TrimSpace(os.Getenv(envDefaultCPU)); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				opt.CPU = f
			}
		}
	}
	if opt.PidsLimit == 0 {
		if v := strings.TrimSpace(os.Getenv(envDefaultPidsLimit)); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != 0 {
				opt.PidsLimit = n
			}
		}
	}
}
