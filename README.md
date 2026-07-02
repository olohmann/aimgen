# aimgen

A small, stdlib-first Go CLI that generates images via the Azure AI Foundry
images endpoint (`POST /openai/v1/images/generations`). It builds the request,
shows a liveliness spinner while the blocking call runs, decodes the returned
`b64_json` payload(s), and writes the PNG(s) to disk.

## Build & install

```bash
make build        # -> ./bin/aimgen
# or
make install      # -> $GOBIN (or $GOPATH/bin)
```

Requires Go 1.25+. The only external dependency is
[`github.com/BurntSushi/toml`](https://github.com/BurntSushi/toml); everything
else is standard library.

## Repository layout

```
.
├── main.go config.go client.go spinner.go   # CLI source
├── *_test.go                                 # unit tests (httptest-based)
├── Makefile                                  # build / test / lint / install
├── go.mod / go.sum
├── aimgen-plan.md                            # original design brief
└── .agents/skills/aimgen/SKILL.md            # agent skill (text-to-image trigger)
```

The Go module lives at the repo root (module `aimgen`). The accompanying
[agent skill](.agents/skills/aimgen/SKILL.md) teaches an AI assistant when and
how to invoke the CLI for image-generation requests.

## Usage

```bash
# Token from the environment (recommended)
export AZURE_AI_TOKEN="$(aitoken)"
aimgen "A photograph of a red fox in an autumn forest"

# Explicit output path and multiple images
aimgen -n 3 -o fox.png "A red fox in an autumn forest"
# -> fox_1.png, fox_2.png, fox_3.png

# Token via flag, custom size and format
aimgen --token "$TOKEN" --size 1024x1536 --format png "A neon city at night"

# Let aimgen fetch the token itself via a shell command (keeps the binary simple)
aimgen --token-command "az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv" \
  "A red fox in an autumn forest"
```

The output filename(s) are printed to **stdout**; the spinner and any status or
error text go to **stderr**, so piping stays clean.

## Configuration

Precedence (highest → lowest): **CLI flag → environment variable → config file →
built-in default**.

Default config file: `$XDG_CONFIG_HOME/aimgen/config.toml`
(`~/.config/aimgen/config.toml`). Override with `--config <path>`.

Generate a commented starter config:

```bash
aimgen --init-config
```

| Setting       | Flag            | Env               | Config key           | Default |
|---------------|-----------------|-------------------|----------------------|---------|
| Endpoint      | `--endpoint`    | `AIMGEN_ENDPOINT` | `endpoint`           | `https://YOUR_RESOURCE.services.ai.azure.com` |
| Token         | `--token`       | `AZURE_AI_TOKEN`  | `token` (discouraged)| — (required) |
| Token command | `--token-command` | `AZURE_AI_TOKEN_COMMAND` | `token_command`  | — |
| Model         | `--model`       | `AIMGEN_MODEL`    | `model`              | `gpt-image-2` |
| Size          | `--size`        | —                 | `size`               | `1024x1024` |
| Output format | `--format`      | —                 | `output_format`      | `png` |
| Count         | `-n`            | —                 | `n`                  | `1` |
| Compression   | `--compression` | —                 | `output_compression` | `100` |
| Output path   | `-o`/`--out`    | —                 | —                    | `generated_image.png` |
| API path      | —               | —                 | `api_path`           | `/openai/v1/images/generations` |
| Timeout (s)   | `--timeout`     | —                 | `timeout_seconds`    | `120` |

Other flags: `--prompt` (alternative to the positional arg), `--quiet` (no
spinner), `--verbose` (redacted request/response summary), `--init-config`,
`--help`.

## Behavior

- **Token resolution:** flag → env → config; if still empty, the `token_command`
  (flag `--token-command` / env `AZURE_AI_TOKEN_COMMAND` / config `token_command`)
  is run via `sh -c` and its trimmed stdout is used. If that yields nothing,
  exits non-zero.
- **Spinner:** elapsed-time spinner on stderr while awaiting the response; auto-
  disabled when stderr is not a TTY or `--quiet` is set.
- **n > 1:** writes `<stem>_1.png`, `<stem>_2.png`, … honoring `-o`.
- **Errors:** non-2xx responses are parsed and printed as `code: message`.

### Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success |
| `1`  | Usage / configuration error (missing prompt, missing token, bad config) |
| `2`  | HTTP / API error (non-2xx, empty image data, write failure) |

## Development

```bash
make help     # list targets
make check    # lint + test + build
make cover    # test with coverage summary
make test     # tests only
```
