# Implementation Plan: `aimgen` — Go CLI for Azure AI Foundry image generation (+ agent skill)

> Self-contained brief for a fresh implementation session. No prior context required.

## Goal
Build a small Go CLI named **`aimgen`** that wraps the Azure AI Foundry images endpoint,
plus an accompanying **agent skill** so the assistant knows when/how to invoke it.

The working reference call this replaces:
```bash
curl -X POST "https://YOUR_RESOURCE.services.ai.azure.com/openai/v1/images/generations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "prompt": "A photograph of a red fox in an autumn forest",
    "model": "gpt-image-2",
    "size": "1024x1024",
    "n": 1,
    "output_format": "png",
    "output_compression": 100
  }' | jq -r '.data[0].b64_json' | base64 --decode > generated_image.png
```
Response shape: `.data[i].b64_json` → base64-decode → write to file.

## Requirements
- Accept a prompt and image params: size, format, count (n), compression.
- Default model `gpt-image-2`.
- Bearer token taken primarily as a `--token` flag, with env-var fallback (`$AZURE_AI_TOKEN`).
- Endpoint configurable via an XDG config file **and** a flag override.
- Show liveliness (spinner + elapsed timer) while the blocking HTTP call runs.
- Decode `data[].b64_json` and write the image(s) to disk.

## Language / placement
- **Go**, single compiled binary, stdlib-first.
- Location: `tools/aimgen/` with its own `go.mod` (module `aimgen`).
- One external dependency: `github.com/BurntSushi/toml` for config parsing (TOML, hand-editable).
  Everything else uses the standard library (`net/http`, `encoding/json`, `flag`, `os`,
  `encoding/base64`).

## Configuration model
**Precedence (highest → lowest):** CLI flag → environment variable → config file → built-in default.

Config file path: `$XDG_CONFIG_HOME/aimgen/config.toml` (default `~/.config/aimgen/config.toml`),
overridable with `--config <path>`.

| Setting        | Flag            | Env                | Config key           | Default |
|----------------|-----------------|--------------------|----------------------|---------|
| Endpoint       | `--endpoint`    | `AIMGEN_ENDPOINT`  | `endpoint`           | `https://YOUR_RESOURCE.services.ai.azure.com` |
| Token          | `--token`       | `AZURE_AI_TOKEN`   | `token` (optional, discouraged) | — (required) |
| Model          | `--model`       | `AIMGEN_MODEL`     | `model`              | `gpt-image-2` |
| Size           | `--size`        | —                  | `size`               | `1024x1024` |
| Output format  | `--format`      | —                  | `output_format`      | `png` |
| Count          | `-n`            | —                  | `n`                  | `1` |
| Compression    | `--compression` | —                  | `output_compression` | `100` |
| Output path    | `-o`/`--out`    | —                  | —                    | `generated_image.png` (index suffix when n>1) |
| API path       | —               | —                  | `api_path`           | `/openai/v1/images/generations` |
| Timeout (s)    | `--timeout`     | —                  | `timeout_seconds`    | `120` |

Additional flags: prompt as positional arg or `--prompt`; `--quiet` (no spinner),
`--verbose` (log request/response summary, redacted token), `--init-config`
(write a commented sample config file), `--help`.

## Behavior details
- **Token resolution:** flag → env → config; if still empty, exit non-zero with a clear message.
- **Liveliness:** spinner + elapsed seconds printed to **stderr** while awaiting the response.
  Auto-disabled when stderr is not a TTY or `--quiet` is set. Image bytes go to the file,
  status text to stderr, so piping stays clean.
- **n > 1:** write `generated_image_1.png`, `generated_image_2.png`, … (honor the `-o` stem).
- **Error handling:** on non-2xx, parse the JSON error body and print `code: message`; exit non-zero.
  On empty `b64_json`, surface the raw response when `--verbose`.
- **Exit codes:** `0` success; `1` usage/config error; `2` HTTP/API error.

## File breakdown (`tools/aimgen/`)
- `go.mod` — module `aimgen` + `BurntSushi/toml` require.
- `main.go` — flag parsing, orchestration, exit codes.
- `config.go` — config struct; load TOML; merge env + defaults; precedence resolution; `--init-config`.
- `client.go` — build request, POST, parse response, decode + write image file(s).
- `spinner.go` — TTY-aware spinner with elapsed timer on stderr.
- `README.md` — usage, config example, build/install notes.

## Accompanying agent skill (Anthropic best practices)
Create `.agents/skills/aimgen/SKILL.md`, matching the repo's existing skill conventions
(see siblings like `.agents/skills/mail/` and `.agents/skills/msx-cli/`). Use the local
**`skill-creator`** skill as the authoring guide.

Requirements:
- **YAML frontmatter:** `name: aimgen` and a tight, **third-person `description`** stating what
  it does *and* explicit trigger conditions ("Use when the user asks to generate/create an image,
  render a picture from a prompt, …") plus a clear non-trigger boundary. High-signal, concise.
- **`allowed-tools: [Bash]`** (matches sibling CLI skills).
- **Progressive disclosure:** short body — overview, when-to-use, the exact command form, key
  flags, token/config setup (point at `aimgen --init-config` and `AZURE_AI_TOKEN` / an `aitoken`
  helper), and a couple of worked examples. Defer long reference to `aimgen --help`; don't duplicate it.
- Imperative, agent-directed instructions; note safe defaults (token via env, default output path).
- Verify documented commands match the built binary's real flags.

## Build & verification
- `cd tools/aimgen && go build ./... && go vet ./...` — clean.
- `aimgen --help` and `aimgen --init-config` work without a token.
- Live smoke test: `AZURE_AI_TOKEN=$(aitoken) aimgen "A red fox in an autumn forest"` produces a
  valid PNG; spinner visible in a TTY, silent when piped.
- Error path: bad token → exit `2` with a parsed API error message.
- Skill check: SKILL.md frontmatter valid; triggers/non-triggers sensible; documented commands
  match the binary.

## Implementation checklist
1. **Scaffold** `tools/aimgen/` — `go.mod` (module `aimgen`), add `BurntSushi/toml`; create file stubs.
2. **Config layer** (`config.go`) — struct, TOML load, env + default merge, precedence, `--init-config`.
3. **Flags + orchestration** (`main.go`) — all flags, token resolution, wire config → client → file writes, exit codes.
4. **HTTP client** (`client.go`) — build body, POST with Bearer + timeout, parse, decode `b64_json`, write file(s), error handling.
5. **Spinner** (`spinner.go`) — TTY-aware liveliness on stderr; respect `--quiet`/non-TTY.
6. **README + verify** — write `README.md`; run `go build`/`go vet`; smoke + error tests.
7. **Agent skill** — author `.agents/skills/aimgen/SKILL.md` per the section above; verify against the binary.

## Open/assumed decisions (change freely)
- Binary/skill name `aimgen`.
- TOML config format (vs JSON/YAML).
- CLI under `tools/aimgen/`; skill under `.agents/skills/aimgen/`.
- `BurntSushi/toml` as the single external dependency; `flag` (stdlib) for arg parsing.
