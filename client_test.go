package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(endpoint string) Config {
	c := defaultConfig()
	c.Endpoint = endpoint
	c.Token = "test-token"
	c.Count = 1
	return c
}

func TestGenerateSuccess(t *testing.T) {
	pngBytes := []byte("\x89PNG fake image bytes")
	encoded := base64.StdEncoding.EncodeToString(pngBytes)

	var gotAuth, gotCT string
	var gotBody generationRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		if r.URL.Path != defaultAPIPath {
			t.Errorf("path = %q, want %q", r.URL.Path, defaultAPIPath)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": encoded}},
		})
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Model = "my-model"
	cfg.Size = "512x512"
	c := newClient(srv.Client(), cfg)

	images, err := c.generate(context.Background(), "a fox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d images, want 1", len(images))
	}
	if string(images[0]) != string(pngBytes) {
		t.Errorf("decoded bytes mismatch")
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.Prompt != "a fox" || gotBody.Model != "my-model" || gotBody.Size != "512x512" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestGenerateAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "401", "message": "invalid token"},
		})
	}))
	defer srv.Close()

	c := newClient(srv.Client(), testConfig(srv.URL))
	_, err := c.generate(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*apiErr)
	if !ok {
		t.Fatalf("error type = %T, want *apiErr", err)
	}
	if ae.code != "401" || ae.message != "invalid token" {
		t.Errorf("apiErr = %+v", ae)
	}
	if got := ae.Error(); got != "401: invalid token" {
		t.Errorf("Error() = %q", got)
	}
}

func TestGenerateEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	c := newClient(srv.Client(), testConfig(srv.URL))
	if _, err := c.generate(context.Background(), "x"); err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestGenerateEmptyB64(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": ""}},
		})
	}))
	defer srv.Close()

	c := newClient(srv.Client(), testConfig(srv.URL))
	if _, err := c.generate(context.Background(), "x"); err == nil {
		t.Fatal("expected error for empty b64_json")
	}
}

func TestOutputNames(t *testing.T) {
	tests := []struct {
		stem  string
		count int
		want  []string
	}{
		{"generated_image.png", 1, []string{"generated_image.png"}},
		{"img.png", 2, []string{"img_1.png", "img_2.png"}},
		{"out/pic.jpeg", 3, []string{"out/pic_1.jpeg", "out/pic_2.jpeg", "out/pic_3.jpeg"}},
		{"noext", 2, []string{"noext_1", "noext_2"}},
	}
	for _, tt := range tests {
		got := outputNames(tt.stem, tt.count)
		if len(got) != len(tt.want) {
			t.Errorf("outputNames(%q,%d) len = %d, want %d", tt.stem, tt.count, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("outputNames(%q,%d)[%d] = %q, want %q", tt.stem, tt.count, i, got[i], tt.want[i])
			}
		}
	}
}

func TestWriteImages(t *testing.T) {
	dir := t.TempDir()
	stem := filepath.Join(dir, "out.png")
	images := [][]byte{[]byte("one"), []byte("two")}

	paths, err := writeImages(stem, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(paths))
	}
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		if string(data) != string(images[i]) {
			t.Errorf("file %s content mismatch", p)
		}
	}
}

func TestParseAPIErrorNonJSON(t *testing.T) {
	err := parseAPIError(500, []byte("internal server error"))
	ae, ok := err.(*apiErr)
	if !ok {
		t.Fatalf("type = %T", err)
	}
	if ae.Error() != "HTTP 500" {
		t.Errorf("Error() = %q, want HTTP 500", ae.Error())
	}
}

func TestContentTypeFor(t *testing.T) {
	tests := map[string]string{
		"a.png":          "image/png",
		"a.PNG":          "image/png",
		"photo.jpg":      "image/jpeg",
		"photo.jpeg":     "image/jpeg",
		"art.webp":       "image/webp",
		"blob":           "application/octet-stream",
		"weird.bmp":      "application/octet-stream",
		"dir/nested.png": "image/png",
	}
	for path, want := range tests {
		if got := contentTypeFor(path); got != want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestEditSuccess(t *testing.T) {
	inputBytes := []byte("\x89PNG source image")
	outBytes := []byte("\x89PNG edited image")
	encoded := base64.StdEncoding.EncodeToString(outBytes)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "input.png")
	if err := os.WriteFile(imgPath, inputBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	var gotPath, gotCT, gotPrompt, gotModel string
	var gotImage []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		gotPrompt = r.FormValue("prompt")
		gotModel = r.FormValue("model")
		if fhs := r.MultipartForm.File["image"]; len(fhs) == 1 {
			f, _ := fhs[0].Open()
			gotImage, _ = io.ReadAll(f)
			f.Close()
		} else {
			t.Errorf("image parts = %d, want 1", len(fhs))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": encoded}},
		})
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.Model = "gpt-image-2"
	c := newClient(srv.Client(), cfg)

	images, err := c.edit(context.Background(), "make it blue", []string{imgPath}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images) != 1 || string(images[0]) != string(outBytes) {
		t.Fatalf("decoded bytes mismatch: %q", images)
	}
	if gotPath != defaultEditPath {
		t.Errorf("path = %q, want %q", gotPath, defaultEditPath)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("content-type = %q, want multipart/form-data", gotCT)
	}
	if gotPrompt != "make it blue" || gotModel != "gpt-image-2" {
		t.Errorf("prompt=%q model=%q", gotPrompt, gotModel)
	}
	if string(gotImage) != string(inputBytes) {
		t.Errorf("uploaded image bytes mismatch")
	}
}

func TestEditMultipleImagesAndMask(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("img"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	maskPath := filepath.Join(dir, "mask.png")
	if err := os.WriteFile(maskPath, []byte("mask"), 0o644); err != nil {
		t.Fatal(err)
	}

	var imageCount, maskCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		imageCount = len(r.MultipartForm.File["image[]"])
		maskCount = len(r.MultipartForm.File["mask"])
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString([]byte("x"))}},
		})
	}))
	defer srv.Close()

	c := newClient(srv.Client(), testConfig(srv.URL))
	if _, err := c.edit(context.Background(), "combine", paths, maskPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imageCount != 2 {
		t.Errorf("image[] parts = %d, want 2", imageCount)
	}
	if maskCount != 1 {
		t.Errorf("mask parts = %d, want 1", maskCount)
	}
}

func TestEditAPIError(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "input.png")
	if err := os.WriteFile(imgPath, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "400", "message": "bad image"},
		})
	}))
	defer srv.Close()

	c := newClient(srv.Client(), testConfig(srv.URL))
	_, err := c.edit(context.Background(), "x", []string{imgPath}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if ae, ok := err.(*apiErr); !ok || ae.code != "400" {
		t.Errorf("error = %v (%T)", err, err)
	}
}
