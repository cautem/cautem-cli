<h1 align="center">cautem-cli</h1>

<p align="center">
  <strong>Agent sandbox CLI</strong><br>
  Create, harden, and operate policy-bound sandboxes — Cursor, Claude, Codex, and BYOC.
</p>
<p align="center">
  <a href="https://github.com/cautem/quarshell-cli/actions/workflows/ci.yml"><img src="https://github.com/cautem/quarshell-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/cautem/quarshell-cli/releases"><img src="https://img.shields.io/github/v/release/cautem/cautem-cli?include_prereleases&sort=semver&label=release" alt="release"></a>
  <a href="https://img.shields.io/badge/status-v0.2.1-blue"><img src="https://img.shields.io/badge/status-v0.2.1-blue" alt="v0.2.1"></a>
  <a href="https://www.apache.org/licenses/LICENSE-2.0"><img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg" alt="License"></a>
  <a href="https://github.com/cautem/quarshell-cli"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go" alt="Go Version"></a>
</p>
<p align="center">
  <sub>Part of the <a href="https://github.com/cautem">cautem / cautem</a> ecosystem</sub>
</p>

---

## Overview

The [quick start](https://cautem.github.io/sandbox.dev/get-started/) contains the supported installation and first-sandbox flow.

**cautem-cli** is the user-facing `cautem` binary for the cautem ecosystem: sandbox lifecycle, policy checks, provider attach, gateway selection, live logs, and agent images.

### Key Features

| Category | Capabilities |
|----------|--------------|
| **Sandboxes** | create / exec / connect / rm — Docker-first via `cautem-driver` |
| **Policy** | YAML check/compose with `cautem-core` + provider presets |
| **Agents** | `--from cursor\|claude\|codex\|base\|gui\|gpu\|gui-gpu` (GHCR or local) |
| **Providers** | Cursor, GitHub, NVIDIA, local inference (`host.cautem.internal`) |
| **Observability** | `cautem logs --tail`, TUI (`cautem term`), OCSF audit lines |
| **Gateway** | register with `cautem-gateway`, relay exec, policy proposals |

---

## Installation

Install the current release (`v0.2.1`) into `~/.local/bin`:

```bash
curl -LsSf https://raw.githubusercontent.com/cautem/cautem-cli/main/install.sh \
  | CAUTEM_VERSION=v0.2.1 sh
```

From source in the [multi-repo workspace](https://github.com/cautem):

```bash
go build -C cautem-cli -o ../cautem ./cmd/cautem
go build -C cautem-cli -o ../cautem-console ./cmd/cautem-console
./cautem install
```

**Requirements:** Docker or Podman. Go 1.27+ only if building from source.

`v0.2.1` is the current stable numbered release. It builds from published module dependencies; OpenShell behavioral compatibility remains partial. See [development and releases](https://cautem.github.io/sandbox.dev/reference/development/) and the [compatibility status](https://cautem.github.io/sandbox.dev/reference/openshell-compatibility/).

---

## Quick Start

```bash
./cautem status
./cautem policy check ./policies/default.yaml

./cautem sandbox create --name demo --workspace . --policy ./policies/default.yaml
./cautem sandbox exec demo -- uname -a
./cautem sandbox rm demo
```

Create on the selected Gateway compute backend with a published OCI image or a
workspace-scoped shared template:

```bash
./cautem sandbox template create shared-agent --gateway \
  --image ghcr.io/example/agent:stable --cpu 2 --memory 4Gi --env MODE=fast
./cautem sandbox create --remote --gateway-workspace team \
  --name agent --image ghcr.io/example/agent:stable \
  --cpu 500m --memory 2Gi --gpu 1 --env MODE=fast
```

Gateway create does not use the local Docker/Podman driver. Image based remote
creates accept CPU, memory, GPU count, environment, labels and per-driver config
overrides. A shared template owns its workload settings and rejects inline
workload overrides. A remote main command currently runs detached because
foreground attach has not been connected yet.

To reconnect to a detached canonical process, run `cautem sandbox attach demo`.
Add `--tty` for a process created with `sandbox create --tty --detach`; attach
reconnects stdin, output, terminal resize, and the process exit status.

Testcontainers cleanup is exposed through the CLI rather than a broad Docker
prune:

```bash
./cautem doctor cleanup --dry-run
./cautem -o json doctor cleanup --yes
```

Only dangling anonymous test volumes and stopped containers labeled
`cautem=1` are in scope.

### Standalone Go console (Cautem transition)

The older server-rendered Go console ships as `cautem-console` in Linux and
macOS Cautem release archives and is installed by `install.sh`. It uses the
public Go SDK and keeps OIDC tokens in server-side sessions. The target browser
console is the separate PWA in [`cautem-ui`](https://github.com/cautem/quarshell-ui),
which calls the Gateway API through its typed client and does not run the CLI. The Go
console remains during the transition and will leave CLI release packaging
after the PWA acceptance in plan 2. Local development can use loopback auth:

```bash
cautem-console \
  -listen 127.0.0.1:8080 \
  -public-url http://127.0.0.1:8080 \
  -gateway http://127.0.0.1:7443
```

For a network deployment, set an HTTPS `-public-url`, configure gateway OIDC,
and publish the console and gateway through the same trusted ingress. The
console supports workspace inventory, overview, sandbox detail/logs, and
create/start/stop/delete actions. The gateway remains the authorization and
policy boundary.

### Cursor agent

```bash
./cautem sandbox create --name cursor --from cursor --workspace "$PWD" \
  --provider cursor --provider gh --policy ./policies/cursor.yaml
./cautem sandbox connect cursor -- agent
```

Agent Dockerfiles live in [`docker/agents/`](./docker/agents/). Policies: [`policies/`](./policies/).
Runnable recipes live in [`examples/`](./examples/).

---

## Package Structure

| Path | Purpose |
|------|---------|
| `cmd/cautem` | CLI entrypoint |
| `cmd/cautem-console` | Go browser console entrypoint |
| `internal/app` | Commands (sandbox, proxy, gateway, rules, …) |
| `internal/console` | Server-side sessions, OIDC PKCE, handlers, templates, and assets |
| `policies/` | Builtin policy YAML |
| `docker/agents/` | cursor / claude / codex images |


---

## Related

| Resource | Link |
|----------|------|
| Roadmap | [ROADMAP.md](./ROADMAP.md) |
| Organization | [https://github.com/cautem](https://github.com/cautem) |
| Organization overview | [github.com/cautem](https://github.com/cautem) |
| CLI source | [`cmd/cautem/`](./cmd/cautem/) |

## License

[Apache-2.0](./LICENSE) © cautem
