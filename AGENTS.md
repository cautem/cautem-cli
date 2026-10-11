# Agent instructions

## Scope

This repository contains the `cautem` CLI, built-in policies, agent container
recipes, and end-to-end tooling. The gateway, runtime, driver, proxy, and core
policy engine live in separate repositories.

## Working rules

- Keep CLI flags, config defaults, policy examples, and user documentation in
  sync. Update English and Russian docs together when behavior changes.
- Treat policy, workspace mount, credential, and process execution paths as
  security-sensitive. Preserve explicit validation and error reporting.
- Keep local Go workspace replacements in the parent workspace's `go.work`,
  never in this repository's `go.mod`.
- Do not edit generated files by hand; update their source and generation steps.

## Checks

- Run `go test ./...` from this checkout for Go changes.
- Run the relevant CLI integration or container checks when changing lifecycle
  behavior; these may require Docker or Podman.
- From the parent workspace, `task check` runs the shared Go gate and
  `task docs:strict` checks the bilingual documentation site.
