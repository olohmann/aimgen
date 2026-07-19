# aimgen

`aimgen` is a small, stdlib-first Go CLI that turns a **text prompt into image
file(s)** via the Azure AI Foundry images endpoint
(`POST /openai/v1/images/generations`). It ships with an **agent skill** so AI
coding agents (GitHub Copilot, Claude Code, Cursor, Codex, …) can invoke it
automatically whenever you ask them to generate a picture.

This README focuses on **installing aimgen as a skill** in your agent tool
chain. For the original design brief see [`aimgen-plan.md`](aimgen-plan.md).

## How it works

Installing aimgen as a skill has two moving parts:

- **The binary** (`aimgen`) — the tool that actually calls Azure and writes
  PNGs. It must be on your `PATH`.
- **The skill** ([`.agents/skills/aimgen/SKILL.md`](.agents/skills/aimgen/SKILL.md))
  — instructions that teach your agent *when* to reach for aimgen and *how* to
  invoke it. The skill just shells out to the binary.

So the flow is: **install the binary → configure Azure → register the skill.**
Then ask your agent for an image and it runs `aimgen` for you.

## 1. Prerequisites

### Install the binary

Requires **Go 1.25+**. The only external dependency is
[`BurntSushi/toml`](https://github.com/BurntSushi/toml); everything else is
standard library.

```bash
go install github.com/olohmann/aimgen@latest   # -> $GOBIN (or $GOPATH/bin)
aimgen --help                                  # verify it's on PATH
```

Prefer building from a clone? See [Build from source](#build-from-source).

### Configure the Azure endpoint and token

Write a commented starter config, then set your resource endpoint:

```bash
aimgen --init-config    # writes ~/.config/aimgen/config.toml
# then edit it: endpoint = "https://YOUR_RESOURCE.services.ai.azure.com"
```

Provide a bearer token at run time (never commit it):

```bash
# Resolve a token into the environment (recommended)
export AZURE_AI_TOKEN="$(az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv)"

# …or let aimgen fetch it lazily on each run
export AZURE_AI_TOKEN_COMMAND="az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv"
```

Quick smoke test (outside any agent):

```bash
aimgen -o fox.png "A photograph of a red fox in an autumn forest"
```

## 2. Install the skill — Microsoft APM (Agent Package Manager)

[APM](https://github.com/microsoft/apm) is a dependency manager for AI agents:
declare agent context in `apm.yml`, then `apm install` reproduces it across
every detected harness (Copilot, Claude, Cursor, …).

Install the APM CLI:

```bash
# macOS / Linux
curl -sSL https://aka.ms/apm-unix | sh
# Windows (PowerShell)
irm https://aka.ms/apm-windows | iex
```

Add the aimgen skill to your project. The skill folder ships an `apm.yml`, so
APM installs it as a versioned **HYBRID skill bundle** and pins it in
`apm.lock.yaml`:

```bash
apm install olohmann/aimgen/.agents/skills/aimgen
```

If your project has no harness directory yet (`.github/`, `.claude/`, …), name
a target explicitly (once one exists, APM auto-detects it):

```bash
apm install olohmann/aimgen/.agents/skills/aimgen --target copilot
# targets: copilot | claude | cursor | codex | gemini | windsurf | kiro
```

Or declare it in `apm.yml` and run `apm install`:

```yaml
dependencies:
  apm:
    - olohmann/aimgen/.agents/skills/aimgen
```

The skill lands under `.agents/skills/aimgen/` (plus any harness-specific dirs
APM detects), ready for your agent to discover on the next session.

## 3. Install the skill — Vercel skills (`npx skills`)

The [open agent-skills CLI](https://www.npmjs.com/package/skills)
(`vercel-labs/skills`) installs `SKILL.md` skills into 70+ agents. aimgen
already lives at the CLI's universal path (`.agents/skills/`), so a direct-path
install always works:

```bash
npx skills add https://github.com/olohmann/aimgen/tree/main/.agents/skills/aimgen
```

Handy options:

```bash
npx skills add olohmann/aimgen --list                  # browse skills in the repo
npx skills add olohmann/aimgen -a claude-code -a codex # target specific agents
npx skills add olohmann/aimgen -g                      # install globally (~/…) instead of per-project
```

By default the skill is symlinked (or copied with `--copy`) into each selected
agent's project skills directory — e.g. `.claude/skills/` for Claude Code,
`.agents/skills/` for universal agents; `-g` installs to the user-level
equivalent.

## 4. Use it from your agent

With the binary, token, and skill in place, just ask in natural language:

> "Generate an image of a neon-lit cyberpunk alley in the rain."

The skill triggers, the agent runs `aimgen`, and the written file path(s) are
printed to **stdout** (status and errors go to stderr, so piping stays clean).
If the binary is missing or the token/endpoint is unset, aimgen fails fast with
a clear message and the skill surfaces it to you.

## Reference

### Configuration precedence

Highest → lowest: **CLI flag → environment variable → config file → built-in
default.** Default config file: `$XDG_CONFIG_HOME/aimgen/config.toml`
(`~/.config/aimgen/config.toml`); override with `--config <path>`.

| Setting       | Flag              | Env                      | Default |
|---------------|-------------------|--------------------------|---------|
| Endpoint      | `--endpoint`      | `AIMGEN_ENDPOINT`        | `https://YOUR_RESOURCE.services.ai.azure.com` |
| Token         | `--token`         | `AZURE_AI_TOKEN`         | — (required) |
| Token command | `--token-command` | `AZURE_AI_TOKEN_COMMAND` | — |
| Model         | `--model`         | `AIMGEN_MODEL`           | `gpt-image-2` |
| Size          | `--size`          | —                        | `1024x1024` |
| Output format | `--format`        | —                        | `png` |
| Count         | `-n`              | —                        | `1` |
| Compression   | `--compression`   | —                        | `100` |
| Output path   | `-o` / `--out`    | —                        | `generated_image.png` |
| Timeout (s)   | `--timeout`       | —                        | `120` |

Other flags: `--prompt` (alternative to the positional arg), `--quiet` (no
spinner), `--verbose` (redacted request/response summary), `--init-config`,
`--help`. With `-n > 1`, output files become `<stem>_1.png`, `<stem>_2.png`, …
Run `aimgen --help` for the authoritative flag list.

### Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success |
| `1`  | Usage / configuration error (missing prompt, missing token, bad config) |
| `2`  | HTTP / API error (non-2xx, empty image data, write failure) |

## Build from source

```bash
git clone https://github.com/olohmann/aimgen.git
cd aimgen
make build      # -> ./bin/aimgen
make install    # -> $GOBIN (or $GOPATH/bin)
```

### Development

```bash
make help     # list targets
make check    # lint + test + build
make cover    # test with coverage summary
make test     # tests only
```
