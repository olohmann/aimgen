package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureRun runs the CLI with a temp HOME/XDG so config lookups are isolated,
// directing stdout/stderr to temp files and returning the exit code plus output.
func captureRun(t *testing.T, args []string, env map[string]string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("HOME", dir)
	t.Setenv("AZURE_AI_TOKEN", "")
	t.Setenv("AIMGEN_ENDPOINT", "")
	t.Setenv("AIMGEN_MODEL", "")
	for k, v := range env {
		t.Setenv(k, v)
	}

	outFile, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatal(err)
	}

	code := run(args, outFile, errFile)

	outFile.Close()
	errFile.Close()
	out, _ := os.ReadFile(outFile.Name())
	errOut, _ := os.ReadFile(errFile.Name())
	return code, string(out), string(errOut)
}

func TestRunHelp(t *testing.T) {
	code, _, stderr := captureRun(t, []string{"--help"}, nil)
	if code != exitOK {
		t.Errorf("code = %d, want %d", code, exitOK)
	}
	if len(stderr) == 0 {
		t.Error("expected help text on stderr")
	}
}

func TestRunVersion(t *testing.T) {
	code, stdout, _ := captureRun(t, []string{"--version"}, nil)
	if code != exitOK {
		t.Errorf("code = %d, want %d", code, exitOK)
	}
	if !strings.HasPrefix(stdout, "aimgen ") {
		t.Errorf("stdout = %q, want it to start with %q", stdout, "aimgen ")
	}
	if !strings.Contains(stdout, version) {
		t.Errorf("stdout = %q, want it to contain version %q", stdout, version)
	}
}

func TestRunMissingPrompt(t *testing.T) {
	code, _, _ := captureRun(t, []string{"--token", "t"}, nil)
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
}

func TestRunMissingToken(t *testing.T) {
	code, _, stderr := captureRun(t, []string{"a prompt"}, nil)
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if stderr == "" {
		t.Error("expected error message")
	}
}

func TestRunInitConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	code, _, _ := captureRun(t, []string{"--config", path, "--init-config"}, nil)
	if code != exitOK {
		t.Fatalf("code = %d, want %d", code, exitOK)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}
}

func TestRunSuccess(t *testing.T) {
	pngBytes := []byte("\x89PNG data")
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": encoded}},
		})
	}))
	defer srv.Close()

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "img.png")
	args := []string{
		"--endpoint", srv.URL,
		"--token", "t",
		"--quiet",
		"-o", outPath,
		"a fox",
	}
	code, stdout, stderr := captureRun(t, args, nil)
	if code != exitOK {
		t.Fatalf("code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if stdout == "" {
		t.Error("expected output path on stdout")
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("image not written: %v", err)
	}
	if string(data) != string(pngBytes) {
		t.Error("image content mismatch")
	}
}

func TestRunAPIErrorExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "401", "message": "bad token"},
		})
	}))
	defer srv.Close()

	args := []string{"--endpoint", srv.URL, "--token", "bad", "--quiet", "a fox"}
	code, _, stderr := captureRun(t, args, nil)
	if code != exitAPIError {
		t.Errorf("code = %d, want %d", code, exitAPIError)
	}
	if stderr == "" {
		t.Error("expected error message")
	}
}

func TestResolvePrompt(t *testing.T) {
	if got := resolvePrompt("flag prompt", []string{"pos", "arg"}); got != "flag prompt" {
		t.Errorf("got %q, want flag prompt", got)
	}
	if got := resolvePrompt("", []string{"a", "red", "fox"}); got != "a red fox" {
		t.Errorf("got %q, want joined args", got)
	}
	if got := resolvePrompt("", nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRunMaskWithoutImage(t *testing.T) {
	code, _, stderr := captureRun(t, []string{"--token", "t", "--mask", "m.png", "a fox"}, nil)
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if stderr == "" {
		t.Error("expected error message")
	}
}

func TestRunMissingImageFile(t *testing.T) {
	code, _, stderr := captureRun(t, []string{"--token", "t", "--image", "/no/such/file.png", "a fox"}, nil)
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if stderr == "" {
		t.Error("expected error message")
	}
}

func TestRunEditSuccess(t *testing.T) {
	inputBytes := []byte("\x89PNG input")
	outBytes := []byte("\x89PNG edited")
	encoded := base64.StdEncoding.EncodeToString(outBytes)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": encoded}},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.png")
	if err := os.WriteFile(inPath, inputBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.png")

	args := []string{
		"--endpoint", srv.URL,
		"--token", "t",
		"--quiet",
		"--image", inPath,
		"-o", outPath,
		"make it blue",
	}
	code, stdout, stderr := captureRun(t, args, nil)
	if code != exitOK {
		t.Fatalf("code = %d, want %d (stderr: %s)", code, exitOK, stderr)
	}
	if gotPath != defaultEditPath {
		t.Errorf("server path = %q, want %q", gotPath, defaultEditPath)
	}
	if stdout == "" {
		t.Error("expected output path on stdout")
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("image not written: %v", err)
	}
	if string(data) != string(outBytes) {
		t.Error("image content mismatch")
	}
}

func TestRedact(t *testing.T) {
	if redact("") != "(none)" {
		t.Error("empty token should redact to (none)")
	}
	if redact("short") != "***" {
		t.Error("short token should be ***")
	}
	got := redact("abcdefghij")
	if got == "abcdefghij" {
		t.Error("token not redacted")
	}
}
