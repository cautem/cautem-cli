# Changelog

## [Unreleased]

### Fixed

- Package the canonical built-in provider profiles with CLI and standalone Gateway archives, install them beside the CLI, and remove the duplicated CLI-owned catalog.

## [v0.3.0] - 2026-10-16

### Added

- Print stdout and stderr returned by compute drivers, including VM non-interactive exec output and truncation notices.
- Allow the VM host policy proxy to keep the Gateway URL in host network context instead of rewriting it for a container sidecar.
- Serve the VM policy proxy on private Unix sockets when linked against published `cautem-proxy` releases.
- Add local process execution, terminal resize, workspace listing, resource-aware sandbox create, and file upload/download flows.

## [v0.2.1] - 2026-10-13

### Changed

- Update cautem-driver and cautem-runtime to v0.2.1 for release builds.

### Fixed

- Expire the Console session cookie on logout in Chrome and other browser clients.

## [v0.2.0] - 2026-10-13

### Added

- Add Gateway login and remote management commands for sandbox, policy, provider, and proxy workflows.
- Add a separately buildable proxy image and publish workflow.

### Changed

- Move generated first-party API clients to the published `cautem-proto` module.
- Split CLI and service operations into focused files while retaining existing command entry points.

## [v0.1.9] - 2026-10-11

### Changed

- Resolve gateway `v0.1.7`, runtime `v0.1.8`, proxy `v0.1.8`, and driver `v0.1.7` in release builds.
- Refresh Go crypto, system, and terminal dependencies.
- Keep the OpenShell SDK schema aligned with the gateway API used by this release.

### Fixed

- Repair the pkg.go.dev link in the README.

## [v0.1.8] - 2026-10-11

### Fixed

- Pin the v0.1.7 driver module in the GoReleaser workspace so release binaries include the default sandbox image fix.

## [v0.1.7] - 2026-10-11

### Changed

- Update cautem-driver to v0.1.7 so first-run sandboxes use the published cautem base when no local image is cached.

## [v0.1.6] - 2026-10-11

### Changed

- Rename the public CLI command, binary, installer and shell completions to `cautem`.

## [v0.1.0-beta.3] - 2026-10-10

### Added

- Add the Go server-rendered management console with OIDC PKCE, secure sessions, workspace views, sandbox details, logs and lifecycle actions.
- Include the console binary in Linux/macOS release archives and `install.sh` layouts.
- Add Podman capability diagnostics and live-Gateway browser smoke coverage.

### Changed

- Resolve Display, Driver, Gateway, Providers, Proxy, Runtime and SDK from published beta tags; CI checks out those exact tag commits.
- Build sandbox agent images in CI from a digest-identical public ECR mirror to avoid Docker Hub rate limits.
- Resolve `cautem-core` v0.1.0-beta.2 and `slogx` v0.1.0-beta.1 from published tags.

### Fixed

- Make provider policy test fixtures self-contained for standalone CLI consumers.

## [v0.1.0-beta.2] - 2026-10-08

### Changed

- Update the driver dependency to v0.1.0-beta.1, including the platform-specific Docker isolation and executable checks.

## [v0.1.0-beta.1] - 2026-10-07

### Added

- Add shell completion scripts and structured command error output.

### Changed

- Use the beta core, proxy, and runtime modules and update Bubble Tea to v1.3.10.

### Fixed

- Protect CLI private files with Windows ACLs and cover Docker cleanup on every CI platform.

## [v0.1.0-alpha.3] - 2026-10-07

### Changed

- Align slogx, display, driver, proxy, and runtime dependencies with their published v0.1.0-alpha.2 modules; update `golang.org/x/crypto` to v0.57.0.
- Refresh the module graph so GoReleaser and cross-platform builds use the same compatible Docker and Moby dependency set as the released driver.

## [v0.1.0-alpha.2] - 2026-09-28

### Added

- Import, validate, update, and resolve OpenShell-compatible provider profiles from local files or the gateway catalog.
- Carry profile-declared OAuth2 refresh configuration and output-token mappings into gateway-managed providers.

### Changed

- Use current GoReleaser archive IDs so the pinned release tooling accepts the CLI and gateway archive configuration.

## [v0.0.2-alpha.1] - 2026-09-28

### Security

- `rule approve-all` skips security-flagged proposals unless `--include-security-flagged` is set.

### Changed

- Prefer `CAUTEM_*` env over legacy `OPENSHELL_*` when both are set.
- Doctor warns on KEK migration need and binary-scoped identity limits on Docker Desktop.

### Fixed

- GoReleaser archive helpers use `strip_parent` so paths land at `libexec/cautem/linux-<arch>/`.
