# cautem-cli/docker/

Agent sandbox images for the `cautem` CLI (`--from cursor|claude|codex`).
Runtime base (`cautem-sandbox:local` / GHCR `:cli`) is built from the [runtime image sources](https://github.com/cautem/cautem-runtime/tree/main/images/sandbox) via `task runtime:image:cli` in the multi-repo workspace.

The Kubernetes companion uses a separate minimal image with the `cautem proxy`
entrypoint. Build it locally from the workspace with `task docker:proxy`; the
default tag is `cautem-proxy:local`. It runs as UID 10001, keeps its policy in
`/etc/cautem/policy.yaml`, and writes audit files below `/tmp/cautem-logs`.
The CLI release workflow publishes a linux/amd64 and linux/arm64 manifest at
`ghcr.io/cautem/cautem/sandboxes/proxy:<version>` after the matching CLI
release. Kubernetes deployments should pin the published image by digest once
the runtime Pod template is enabled.

| Path | Tag | Contents |
|------|-----|----------|
| [`agents/cursor`](./agents/cursor/) | `cautem-sandbox:cursor` | + Cursor Agent CLI under `/opt/cursor-agent` |
| [`agents/claude`](./agents/claude/) | `cautem-sandbox:claude` | + Claude Code CLI |
| [`agents/codex`](./agents/codex/) | `cautem-sandbox:codex` | + OpenAI Codex CLI |

Published images and BYOC: [image reference](https://cautem.github.io/sandbox.dev/reference/images/).

## Build

```bash
export GOWORK=$PWD/go.work
task runtime:image:cli          # base → cautem-sandbox:local
task docker:agent:cursor        # → cautem-sandbox:cursor
task docker:agent:claude
task docker:agent:codex
task docker:agent:all
task docker:proxy
```

## Use

```bash
cautem sandbox create --name cursor \
  --from cursor \
  --workspace . \
  --policy cautem-cli/policies/cursor.yaml \
  -- agent
```
