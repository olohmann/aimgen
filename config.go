package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Built-in defaults. Lowest precedence in the resolution chain.
const (
	defaultEndpoint    = "https://YOUR_RESOURCE.services.ai.azure.com"
	defaultModel       = "gpt-image-2"
	defaultSize        = "1024x1024"
	defaultFormat      = "png"
	defaultCount       = 1
	defaultCompression = 100
	defaultAPIPath     = "/openai/v1/images/generations"
	defaultEditPath    = "/openai/v1/images/edits"
	defaultTimeout     = 120
	defaultOutput      = "generated_image.png"
)

// Config holds the fully-resolved settings used to perform a generation.
type Config struct {
	Endpoint     string `toml:"endpoint"`
	Token        string `toml:"token"`
	TokenCommand string `toml:"token_command"`
	Model        string `toml:"model"`
	Size         string `toml:"size"`
	Format       string `toml:"output_format"`
	Count        int    `toml:"n"`
	Compression  int    `toml:"output_compression"`
	APIPath      string `toml:"api_path"`
	EditPath     string `toml:"edit_api_path"`
	Timeout      int    `toml:"timeout_seconds"`
}

// defaultConfig returns a Config populated with the built-in defaults.
func defaultConfig() Config {
	return Config{
		Endpoint:    defaultEndpoint,
		Model:       defaultModel,
		Size:        defaultSize,
		Format:      defaultFormat,
		Count:       defaultCount,
		Compression: defaultCompression,
		APIPath:     defaultAPIPath,
		EditPath:    defaultEditPath,
		Timeout:     defaultTimeout,
	}
}

// configHome returns the base XDG config directory, honoring XDG_CONFIG_HOME
// and falling back to ~/.config.
func configHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// defaultConfigPath returns the default config file location.
func defaultConfigPath() (string, error) {
	base, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "aimgen", "config.toml"), nil
}

// loadFileConfig reads and decodes a TOML config file. A missing file is not an
// error: it returns an empty Config and found=false.
func loadFileConfig(path string) (Config, bool, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, true, nil
}

// mergeEnv overlays environment-variable values onto c (env beats file/default).
func mergeEnv(c *Config, env func(string) string) {
	if v := env("AIMGEN_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if v := env("AZURE_AI_TOKEN"); v != "" {
		c.Token = v
	}
	if v := env("AZURE_AI_TOKEN_COMMAND"); v != "" {
		c.TokenCommand = v
	}
	if v := env("AIMGEN_MODEL"); v != "" {
		c.Model = v
	}
}

// stringFlags carries the raw CLI flag values. Empty/zero means "unset" for the
// string fields; the *Set booleans disambiguate numeric flags that legitimately
// accept zero-ish values.
type stringFlags struct {
	endpoint     string
	token        string
	tokenCommand string
	model        string
	size         string
	format       string
	count        int
	countSet     bool
	compression  int
	compSet      bool
	timeout      int
	timeoutSet   bool
}

// mergeFlags overlays explicitly-set CLI flag values onto c (highest precedence).
func mergeFlags(c *Config, f stringFlags) {
	if f.endpoint != "" {
		c.Endpoint = f.endpoint
	}
	if f.token != "" {
		c.Token = f.token
	}
	if f.tokenCommand != "" {
		c.TokenCommand = f.tokenCommand
	}
	if f.model != "" {
		c.Model = f.model
	}
	if f.size != "" {
		c.Size = f.size
	}
	if f.format != "" {
		c.Format = f.format
	}
	if f.countSet {
		c.Count = f.count
	}
	if f.compSet {
		c.Compression = f.compression
	}
	if f.timeoutSet {
		c.Timeout = f.timeout
	}
}

// resolveConfig builds the final Config by applying, in increasing precedence:
// defaults → file → env → flags.
func resolveConfig(path string, flags stringFlags, env func(string) string) (Config, error) {
	c := defaultConfig()

	fileCfg, found, err := loadFileConfig(path)
	if err != nil {
		return Config{}, err
	}
	if found {
		applyFileOverrides(&c, fileCfg)
	}

	mergeEnv(&c, env)
	mergeFlags(&c, flags)
	return c, nil
}

// resolveToken returns the bearer token to use. An explicitly-provided token
// (flag/env/file) takes precedence. Otherwise, if a token command is set, it is
// executed via the shell and its trimmed stdout is used as the token.
func resolveToken(c Config) (string, error) {
	if c.Token != "" {
		return c.Token, nil
	}
	if c.TokenCommand == "" {
		return "", nil
	}
	return runTokenCommand(c.TokenCommand)
}

// runTokenCommand executes cmd via the shell and returns its trimmed stdout.
func runTokenCommand(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("token command failed: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("token command failed: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("token command produced no output")
	}
	return token, nil
}

// applyFileOverrides overlays non-empty values from a decoded file config.
func applyFileOverrides(c *Config, f Config) {
	if f.Endpoint != "" {
		c.Endpoint = f.Endpoint
	}
	if f.Token != "" {
		c.Token = f.Token
	}
	if f.TokenCommand != "" {
		c.TokenCommand = f.TokenCommand
	}
	if f.Model != "" {
		c.Model = f.Model
	}
	if f.Size != "" {
		c.Size = f.Size
	}
	if f.Format != "" {
		c.Format = f.Format
	}
	if f.Count != 0 {
		c.Count = f.Count
	}
	if f.Compression != 0 {
		c.Compression = f.Compression
	}
	if f.APIPath != "" {
		c.APIPath = f.APIPath
	}
	if f.EditPath != "" {
		c.EditPath = f.EditPath
	}
	if f.Timeout != 0 {
		c.Timeout = f.Timeout
	}
}

// sampleConfig is the commented template written by --init-config.
const sampleConfig = `# aimgen configuration
# Precedence (highest -> lowest): CLI flag -> environment variable -> this file -> built-in default.

# Azure AI Foundry base endpoint.
endpoint = "` + defaultEndpoint + `"

# Bearer token. Discouraged here; prefer the AZURE_AI_TOKEN env var or --token.
# token = "your-token"

# Shell command that prints the bearer token to stdout. Executed when no token
# is set directly (via token/--token/AZURE_AI_TOKEN). Keeps secrets out of the
# config and lets you delegate to tools like the Azure CLI.
# Env: AZURE_AI_TOKEN_COMMAND, flag: --token-command
# token_command = "az account get-access-token --resource https://ai.azure.com --query accessToken -o tsv"

# Model deployment name.
model = "` + defaultModel + `"

# Image size, e.g. 1024x1024, 1024x1536, 1536x1024.
size = "` + defaultSize + `"

# Output image format: png, jpeg, webp.
output_format = "` + defaultFormat + `"

# Number of images to generate.
n = 1

# Output compression (0-100), applies to lossy formats.
output_compression = 100

# Request path appended to the endpoint.
api_path = "` + defaultAPIPath + `"

# Request path for image edits (used when --image is provided).
edit_api_path = "` + defaultEditPath + `"

# HTTP timeout in seconds.
timeout_seconds = 120
`

// writeSampleConfig writes the commented sample config to path, creating parent
// directories. It refuses to overwrite an existing file.
func writeSampleConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sampleConfig), 0o644)
}
