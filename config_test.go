package main

import (
	"os"
	"path/filepath"
	"testing"
)

func noEnv(string) string { return "" }

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveConfigDefaults(t *testing.T) {
	cfg, err := resolveConfig(filepath.Join(t.TempDir(), "missing.toml"), stringFlags{}, noEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want default", cfg.Endpoint)
	}
	if cfg.Model != defaultModel {
		t.Errorf("model = %q, want %q", cfg.Model, defaultModel)
	}
	if cfg.Count != defaultCount {
		t.Errorf("count = %d, want %d", cfg.Count, defaultCount)
	}
	if cfg.APIPath != defaultAPIPath {
		t.Errorf("api_path = %q, want %q", cfg.APIPath, defaultAPIPath)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %d, want %d", cfg.Timeout, defaultTimeout)
	}
}

func TestResolveConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	fileContent := `
endpoint = "https://file.example"
model = "file-model"
size = "512x512"
n = 2
timeout_seconds = 30
`
	if err := os.WriteFile(path, []byte(fileContent), 0o644); err != nil {
		t.Fatal(err)
	}

	env := envFrom(map[string]string{
		"AIMGEN_ENDPOINT": "https://env.example",
		"AIMGEN_MODEL":    "env-model",
		"AZURE_AI_TOKEN":  "env-token",
	})

	flags := stringFlags{
		model:    "flag-model",
		count:    5,
		countSet: true,
	}

	cfg, err := resolveConfig(path, flags, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// flag beats env beats file
	if cfg.Model != "flag-model" {
		t.Errorf("model = %q, want flag-model", cfg.Model)
	}
	// env beats file (no flag)
	if cfg.Endpoint != "https://env.example" {
		t.Errorf("endpoint = %q, want env value", cfg.Endpoint)
	}
	// token only from env
	if cfg.Token != "env-token" {
		t.Errorf("token = %q, want env-token", cfg.Token)
	}
	// file value where no env/flag
	if cfg.Size != "512x512" {
		t.Errorf("size = %q, want file value", cfg.Size)
	}
	// flag numeric beats file
	if cfg.Count != 5 {
		t.Errorf("count = %d, want 5", cfg.Count)
	}
	// file numeric where no flag
	if cfg.Timeout != 30 {
		t.Errorf("timeout = %d, want 30", cfg.Timeout)
	}
	// default where nothing set
	if cfg.Compression != defaultCompression {
		t.Errorf("compression = %d, want default", cfg.Compression)
	}
}

func TestResolveConfigMissingFileIsOK(t *testing.T) {
	_, err := resolveConfig(filepath.Join(t.TempDir(), "nope.toml"), stringFlags{}, noEnv)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
}

func TestResolveConfigInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("this is = = not toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveConfig(path, stringFlags{}, noEnv); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestWriteSampleConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")

	if err := writeSampleConfig(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("sample config is empty")
	}

	// Refuses to overwrite.
	if err := writeSampleConfig(path); err == nil {
		t.Fatal("expected error overwriting existing config")
	}

	// Sample must be valid and round-trip through the resolver.
	cfg, err := resolveConfig(path, stringFlags{}, noEnv)
	if err != nil {
		t.Fatalf("sample config does not parse: %v", err)
	}
	if cfg.Model != defaultModel {
		t.Errorf("sample model = %q, want %q", cfg.Model, defaultModel)
	}
}

func TestConfigHomeXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/custom/xdg", "aimgen", "config.toml")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestResolveTokenExplicitWins(t *testing.T) {
	tok, err := resolveToken(Config{Token: "direct", TokenCommand: "echo should-not-run"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "direct" {
		t.Errorf("token = %q, want direct", tok)
	}
}

func TestResolveTokenFromCommand(t *testing.T) {
	tok, err := resolveToken(Config{TokenCommand: "echo '  cmd-token  '"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "cmd-token" {
		t.Errorf("token = %q, want cmd-token (trimmed)", tok)
	}
}

func TestResolveTokenNoneSet(t *testing.T) {
	tok, err := resolveToken(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "" {
		t.Errorf("token = %q, want empty", tok)
	}
}

func TestResolveTokenCommandFailure(t *testing.T) {
	if _, err := resolveToken(Config{TokenCommand: "exit 1"}); err == nil {
		t.Fatal("expected error from failing command, got nil")
	}
}

func TestResolveTokenCommandEmptyOutput(t *testing.T) {
	if _, err := resolveToken(Config{TokenCommand: "true"}); err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
}

func TestResolveConfigTokenCommandPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("token_command = \"echo file-cmd\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// File value applies when no env/flag.
	cfg, err := resolveConfig(path, stringFlags{}, noEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TokenCommand != "echo file-cmd" {
		t.Errorf("token_command = %q, want file value", cfg.TokenCommand)
	}

	// Env beats file.
	env := envFrom(map[string]string{"AZURE_AI_TOKEN_COMMAND": "echo env-cmd"})
	cfg, err = resolveConfig(path, stringFlags{}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TokenCommand != "echo env-cmd" {
		t.Errorf("token_command = %q, want env value", cfg.TokenCommand)
	}

	// Flag beats env.
	cfg, err = resolveConfig(path, stringFlags{tokenCommand: "echo flag-cmd"}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TokenCommand != "echo flag-cmd" {
		t.Errorf("token_command = %q, want flag value", cfg.TokenCommand)
	}
}
