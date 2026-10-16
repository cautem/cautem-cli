# GitHub provider — agent push inside a sandbox

Goal: an agent **inside** the sandbox can `git push` / `gh` to your org.
The host only seeds the token once; the guest sees `cautem:resolve:env:GITHUB_TOKEN`,
and the sidecar rewrites it on egress.

**Общий запуск + create:** [профили провайдеров](https://cautem.github.io/sandbox.dev/ru/guides/provider-profiles/)\
**Cursor+Git:** [Docker](https://cautem.github.io/sandbox.dev/ru/providers/docker/).

## Prerequisites

- Docker (or Podman) running on the host
- Go toolchain + this workspace (`go.work`)
- GitHub PAT with **`repo`** (classic) or fine-grained **Contents: Read and write**
  on the target repos
- Empty (or existing) repos under the org, e.g. `cautem/cautem-cli`

## 1. Build CLI + gateway

```bash
cd /path/to/workspace
export GOWORK=$PWD/go.work
go build -C cautem-cli -o ../cautem ./cmd/cautem
go build -C cautem-gateway -o ../cautem-gateway ./cmd/cautem-gateway
./cautem install   # or: export PATH="$PWD:$PATH"
```

## 2. Start gateway + register it

```bash
./cautem-gateway --listen 127.0.0.1:7443 &
./cautem gateway add http://127.0.0.1:7443 --local --name local
./cautem gateway select local
```

## 3. Seed GitHub token (one-shot — not a sticky shell export)

```bash
# PAT only for this process; do not leave export in your interactive shell
GITHUB_TOKEN=ghp_… ./cautem provider create --name gh --type github --credential GITHUB_TOKEN
./cautem provider list   # shows name/type/keys only — never the value
```

## 4. Create a sandbox that can push

Use the write-capable **base policy** from the start (org-scoped `cautem`):

```bash
./cautem sandbox create --name push \
  --workspace "$PWD" \
  --policy cautem-cli/policies/github-push-cautem.yaml \
  --gateway http://127.0.0.1:7443 \
  --provider gh
# If instance missing: use --provider github (auto-creates from profile + $GITHUB_TOKEN)
# or: GITHUB_TOKEN=… ./cautem provider create --name gh --type github --credential GITHUB_TOKEN
```

Composition: **base** = push policy (`git-receive-pack` + API write under `/cautem/**`)
plus **provider-composed** github endpoints/credential binding from `gh`.

Check:

```bash
./cautem policy get push --base    # editable base
./cautem policy get push --full    # effective policy (base + provider-composed)
```

### Alternative: create narrow, then widen

```bash
./cautem sandbox create --name push --workspace "$PWD" \
  --policy cautem-cli/policies/default.yaml \
  --gateway http://127.0.0.1:7443 --provider gh

# push will DENY until:
./cautem policy set push --policy cautem-cli/policies/github-push-cautem.yaml
```

## 5. Run the agent *inside* the sandbox

Do **not** push from the host with a placeholder token. Examples:

```bash
# one-shot command
./cautem sandbox exec push -- git -C /workspace/cautem-cli status

# interactive shell (then run your agent / git / gh)
./cautem sandbox exec push -- bash

# or create-with-command (keeps sandbox)
./cautem sandbox create --name agent --workspace "$PWD" \
  --policy cautem-cli/policies/github-push-cautem.yaml \
  --gateway http://127.0.0.1:7443 --provider gh \
  -- bash
```

Inside the guest, env looks like:

```text
GITHUB_TOKEN=cautem:resolve:env:GITHUB_TOKEN
```

`git` / `gh` / `curl` to `github.com` / `api.github.com` go through the sidecar;
the real PAT is substituted only for credential-bound endpoints.

### First push per module

Repos must exist on GitHub. From **inside** the sandbox:

```bash
cd /workspace/cautem-cli
git remote -v   # should be https://github.com/cautem/quarshell-cli.git
git push -u origin main
```

Repeat for `cautem-core`, `cautem-runtime`, `cautem-gateway` as needed.

## 6. If push is denied

```bash
./cautem logs push --source proxy | grep -E 'DENIED|FINDING'
./cautem policy get push --full | head
# fix base, then:
./cautem policy set push --policy cautem-cli/policies/github-push-cautem.yaml
```

Typical causes:

| Symptom | Fix |
|---------|-----|
| DENY `git-receive-pack` | base missing push rules → `policy set` push YAML |
| `credential_endpoint_mismatch` | provider not attached / wrong key binding |
| GitHub 401 | re-seed: `GITHUB_TOKEN=… cautem provider update gh --credential GITHUB_TOKEN` |
| Host push with placeholder | always `cautem sandbox exec …` — host has no MITM rewrite |

## 7. Cleanup

```bash
./cautem sandbox delete push
# optional: stop gateway job
```

## How policies work

cautem starts **default deny**. Widen the base policy when the agent needs more.

```bash
./cautem gateway ensure   # starts cautem-gateway beside CLI if needed
./cautem sandbox create --from cursor --workspace "$PWD" -- agent
# -- agent infers --provider cursor; gateway ensure runs automatically
```

| Layer | Builtin `github` provider | After base widen (`github-push-cautem.yaml`) |
|-------|---------------------------|-----------------------------------------------|
| API | `read-only` | write under `/repos/cautem/**` + **create repo** |
| Git | `git-upload-pack` (clone/fetch) | + `git-receive-pack` (push) |

Typical loop:

1. Agent tries `gh repo create` / `git push` → proxy **DENIED**
2. Operator reads logs → updates **base policy** (`policy set`)
3. Gateway **composition** keeps provider-composed credentials/endpoints
4. Agent retries inside the sandbox

Create-repo allows in our write base policy:

- `POST /orgs/cautem/repos` — `gh repo create cautem/cautem-cli`
- `POST /user/repos` — user-owned `gh repo create cautem-cli`
- then `git-receive-pack` for push

Token still needs GitHub permission (`repo` / admin on org). Policy only admits the HTTP path; GitHub ACLs still apply.

## Agent create + push (inside sandbox)

```bash
./cautem sandbox exec push -- bash
# inside:
gh repo create cautem/cautem-cli --private --source=/workspace/cautem-cli --remote=origin --push
# or:
gh api -X POST /orgs/cautem/repos -f name=cautem-cli -F private=true
cd /workspace/cautem-cli && git remote add origin https://github.com/cautem/quarshell-cli.git
git push -u origin main
```

If DENIED: `./cautem logs push --source proxy` → adjust base → `./cautem policy set push …`.
