# Kamino

Provision a freshly installed Ubuntu server from a versioned config repository —
a single Go binary with an embedded web UI that pulls your configuration over
HTTPS, shows you exactly what it will install, and streams live progress as it
runs.

> The name is a nod to the Star Wars cloning world: point every server at the
> same config repo and they come out identical, every time.

- **One binary, no dependencies.** The React UI is embedded via `go:embed`; state
  lives in a pure-Go SQLite database (`CGO_ENABLED=0`). Download it, run it.
- **Your config, your repo.** Kamino ships with no configuration baked in. You
  point it at a repo you control (GitHub, GitLab, Gitea/Forgejo, or any raw HTTPS
  host) and it treats that repo as read-only truth.
- **No git, no clone.** Config files are fetched as raw content over HTTPS on
  demand and pinned to a single commit SHA for the run, so a mid-run push can't
  produce a half-old, half-new plan.
- **You see everything first.** The plan lists every package, version, and every
  `script` / `pre_install` / `post_install` command that will execute as root
  before you start.

---

## Table of contents

- [How it works](#how-it-works)
- [Install](#install)
- [First run](#first-run)
- [Command-line usage](#command-line-usage)
  - [Flags](#flags)
  - [Environment variables](#environment-variables)
- [The config repository](#the-config-repository)
- [Secrets](#secrets)
- [Security & trust model](#security--trust-model)
- [Metrics](#metrics)
- [Screenshots](#screenshots)
- [Building from source](#building-from-source)
- [License](#license)

---

## How it works

1. You tell Kamino where your config repo is (URL + branch/ref, plus an access
   token for private repos) — either in the web UI's **Connect** screen or via
   flags/environment for a headless bootstrap.
2. Kamino fetches `manifest.yaml` from the repo's raw base URL, resolves the
   branch to a commit SHA, and fetches every referenced category/profile file at
   that SHA. Scripts and compose-stack files are fetched lazily, only when a step
   that needs them runs.
3. You pick a **profile** (or individual items) and Kamino resolves the
   dependency graph into an ordered plan.
4. Each step runs its idempotency `check` first — an already-installed item is
   marked `skipped` — otherwise the matching installer runs with a per-step
   timeout, streaming stdout/stderr to the UI and SQLite line by line.

Kamino runs its installs as **root** (launch it with `sudo`) and targets
**Ubuntu** on **amd64/arm64**; it refuses to start otherwise, with an actionable
error.

---

## Install

Binaries are published on the [releases page](https://github.com/t0mer/kamino/releases)
for `linux/amd64` and `linux/arm64`. Grab the archive for your architecture,
extract it, and run it with `sudo`.

**amd64:**

```bash
wget https://github.com/t0mer/kamino/releases/latest/download/kamino_linux_amd64.tar.gz
tar xzf kamino_linux_amd64.tar.gz
sudo ./kamino serve
```

**arm64:**

```bash
wget https://github.com/t0mer/kamino/releases/latest/download/kamino_linux_arm64.tar.gz
tar xzf kamino_linux_arm64.tar.gz
sudo ./kamino serve
```

Each release also ships a `checksums.txt`; verify with
`sha256sum -c checksums.txt` after downloading.

---

## First run

```bash
sudo ./kamino serve
```

Kamino listens on `http://127.0.0.1:8844` by default and prints an **API token**
the first time it runs (stored in the data dir; shown only once). Open the UI and:

- On the **Connect** screen, enter your config repo URL, branch (default `main`),
  and — for a private repo — an access token. Hit **Test connection** to confirm
  Kamino can reach it and found a valid manifest before saving.
- On the **Setup** screen, review the host card, pick a profile or individual
  items, fill in any required secrets, and review the plan.
- The **Run** screen streams live progress; **History** keeps the last runs with
  their config SHA, result, and logs.

To reach the UI from another machine, bind a non-loopback address explicitly
(`--listen 0.0.0.0:8844`). Kamino logs a warning when it does, because the API
installs software as root — put it behind a firewall or reverse proxy.

For a fully headless bootstrap (cloud-init, SSH), skip the UI entirely:

```bash
sudo ./kamino apply --repo https://github.com/you/your-config --profile production --yes
```

---

## Command-line usage

```
kamino serve      # HTTP server + embedded web UI (the main lifecycle)
kamino plan       # resolve a profile into an ordered plan and print it
kamino apply      # run an install headlessly, progress streamed to the terminal
kamino validate   # fetch and validate the config repo, print the resolved plans
kamino version    # print the version and exit
```

Typical headless commands:

```bash
kamino validate --repo https://github.com/you/your-config
kamino plan     --repo https://github.com/you/your-config --profile dev
kamino apply    --repo https://github.com/you/your-config --profile dev --continue-on-error --yes
```

### Flags

Persistent flags (available on every subcommand):

| Flag | Default | Purpose |
|---|---|---|
| `--data-dir` | `/var/lib/kamino` | Data directory: SQLite DB, saved settings, offline cache. |
| `--repo` | _(saved setting)_ | Config repo URL. Overrides the value saved through the UI. |
| `--ref` | `main` | Config repo branch, tag, or commit SHA. |
| `--token` | – | Access token for a private config repo. |
| `--raw-base` | – | Raw base URL template (`{ref}`, `{path}`) for non-GitHub/GitLab/Gitea hosts. |
| `--log-level` | `info` | `debug`, `info`, `warning`, or `error`. |
| `--log-format` | `auto` | `json`, `text`, or `auto` (text on a terminal, JSON otherwise). |

Command-specific flags:

| Command | Flag | Default | Purpose |
|---|---|---|---|
| `serve` | `--listen` | `127.0.0.1:8844` | Address to bind. Use `0.0.0.0:8844` to expose it. |
| `plan` | `--profile` | – | Profile to resolve (required). |
| `plan` | `--arch` | host arch | Target architecture (`amd64` / `arm64`). |
| `plan` | `--json` | `false` | Emit the plan as JSON. |
| `apply` | `--profile` | – | Profile to apply (required). |
| `apply` | `--arch` | host arch | Target architecture. |
| `apply` | `--continue-on-error` | `false` | Keep going after a failed step; its dependents are marked blocked. |
| `apply` | `--dry-run` | `false` | Print the plan and exit without installing. |
| `apply` | `--yes` | `false` | Proceed without the confirmation prompt. |
| `apply` | `--verbose` | `false` | Stream command output to the terminal. |
| `apply` | `--secret` | – | Secret value as `KEY=VALUE` (repeatable). |
| `apply` | `--keep-runs` | `50` | How many past runs to retain in history. |
| `validate` | `--ref` | `main` | Ref to validate. |

### Environment variables

Resolution precedence is **flag → environment → saved setting** (a saved UI
value is the normal path; flags and env exist as one-off / headless overrides).

| Variable | Equivalent flag |
|---|---|
| `KAMINO_REPO` | `--repo` |
| `KAMINO_REF` | `--ref` |
| `KAMINO_TOKEN` | `--token` |
| `KAMINO_RAW_BASE` | `--raw-base` |

---

## The config repository

Kamino is driven entirely by a config repo you own. Its layout:

```
your-config/
├── manifest.yaml            # entry point: schema version, defaults, category index
├── categories/*.yaml        # installable items grouped by category
├── profiles/*.yaml          # named selections of items, with version overrides
├── stacks/                  # optional docker-compose stacks
└── scripts/                 # custom install scripts referenced by items
```

Each item declares a `type` — the installer engine supports `apt`, `deb`,
`tarball`, `binary`, `pip`, `script`, and `compose_stack` (with `snap` optional)
— plus fields like `version`, per-arch `source` maps, `depends_on`, idempotency
`check` / `check_contains`, `pre_install` / `post_install` hooks, and `secrets`.

A JSON Schema for these files lives under [`schema/`](schema/) in this repo, so
your config repo can validate itself in CI. `t0mer/kamino-config` is a reference
example you can fork; the app has **no** repo baked in.

Run `kamino validate --repo <url>` to check schema, dependency graph (no cycles,
refs exist), and per-arch source coverage, and to print the resolved plan per
profile. The same validation runs automatically before every apply.

---

## Secrets

Values such as a Cloudflare tunnel token are **declared by name** in the config
repo (never their values) and supplied at run time — through the UI or with
`--secret KEY=VALUE` / environment. They are held in memory for the run only,
injected via `{secret:NAME}` templating, redacted (`***`) from every captured log
line, and never written to SQLite or disk.

---

## Security & trust model

The config repo is arbitrary, user-supplied input, and its scripts run as root —
that is by design (you trust your own repo), so Kamino makes it impossible to do
by accident:

- The Setup screen shows the repo URL and resolved commit **SHA** prominently.
- The plan lists every `script`, `pre_install`, and `post_install` command that
  will execute, before anything runs.
- Changing the repo URL requires an explicit confirmation.
- Every remote download is HTTPS-only; when an item declares a `sha256` it is
  verified, and the plan warns when a checksum is absent.
- Per-step timeouts are always enforced, so a hung `apt` can't wedge the run.
- A config fetch failure falls back to the last cached copy with a visible
  "stale config" warning — a GitHub outage doesn't block a rebuild.
- The API is authenticated with a token; it binds loopback by default.

---

## Metrics

Kamino exposes Prometheus metrics at `GET /metrics` (unauthenticated, like
`/healthz`, since the series carry no secrets):

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `kamino_runs_total` | counter | `status` | Runs that reached a terminal status. |
| `kamino_steps_total` | counter | `status` | Step transitions to a terminal status. |
| `kamino_step_duration_seconds` | histogram | – | Wall-clock duration of steps that ran. |

---

## Screenshots

_Screenshots of the Connect, Setup, Run, and History screens (light and dark)
will be added here._

---

## Building from source

Requires Go (see `go.mod` for the version) and Node 20 for the web UI.

```bash
make ui       # build the React app into internal/webui/dist (go:embed source)
make build    # compile the single binary into bin/kamino
make test     # go test ./...
make vet      # go vet ./...
```

Releases are produced by [goreleaser](https://goreleaser.com) (see
`.goreleaser.yaml`): `linux/amd64` and `linux/arm64` archives plus
`checksums.txt`, and nothing else.

---

## License

[Apache-2.0](LICENSE).
