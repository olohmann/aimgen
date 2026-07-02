package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// generationRequest is the JSON payload sent to the images endpoint.
type generationRequest struct {
	Prompt            string `json:"prompt"`
	Model             string `json:"model"`
	Size              string `json:"size"`
	N                 int    `json:"n"`
	OutputFormat      string `json:"output_format"`
	OutputCompression int    `json:"output_compression"`
}

// generationResponse is the relevant subset of a successful response.
type generationResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}

// apiError is the shape of an Azure/OpenAI error body.
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// apiErr is an error carrying a parsed API failure; it maps to exit code 2.
type apiErr struct {
	status  int
	code    string
	message string
	raw     string
}

func (e *apiErr) Error() string {
	if e.code != "" && e.message != "" {
		return fmt.Sprintf("%s: %s", e.code, e.message)
	}
	if e.message != "" {
		return e.message
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

// client performs image generation requests.
type client struct {
	httpClient *http.Client
	cfg        Config
}

// newClient builds a client from a resolved Config.
func newClient(httpClient *http.Client, cfg Config) *client {
	return &client{httpClient: httpClient, cfg: cfg}
}

// generate posts the prompt and returns the decoded image bytes for each result.
func (c *client) generate(ctx context.Context, prompt string) ([][]byte, error) {
	body, err := json.Marshal(generationRequest{
		Prompt:            prompt,
		Model:             c.cfg.Model,
		Size:              c.cfg.Size,
		N:                 c.cfg.Count,
		OutputFormat:      c.cfg.Format,
		OutputCompression: c.cfg.Compression,
	})
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.cfg.Endpoint, "/") + c.cfg.APIPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, respBody)
	}

	var parsed generationResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, &apiErr{status: resp.StatusCode, message: "response contained no image data", raw: string(respBody)}
	}

	images := make([][]byte, 0, len(parsed.Data))
	for i, d := range parsed.Data {
		if d.B64JSON == "" {
			return nil, &apiErr{status: resp.StatusCode, message: fmt.Sprintf("empty b64_json at index %d", i), raw: string(respBody)}
		}
		decoded, err := base64.StdEncoding.DecodeString(d.B64JSON)
		if err != nil {
			return nil, fmt.Errorf("decoding image %d: %w", i, err)
		}
		images = append(images, decoded)
	}
	return images, nil
}

// parseAPIError extracts a structured error from a non-2xx body.
func parseAPIError(status int, body []byte) error {
	e := &apiErr{status: status, raw: string(body)}
	var ae apiError
	if err := json.Unmarshal(body, &ae); err == nil {
		e.code = ae.Error.Code
		e.message = ae.Error.Message
	}
	return e
}

// outputNames derives output filenames from a stem. For a single image it
// returns the stem unchanged; for multiple it inserts a 1-based index before the
// extension (e.g. img.png -> img_1.png, img_2.png).
func outputNames(stem string, count int) []string {
	if count <= 1 {
		return []string{stem}
	}
	ext := filepath.Ext(stem)
	base := strings.TrimSuffix(stem, ext)
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d%s", base, i+1, ext)
	}
	return names
}

// writeImages writes each image to a derived filename and returns the paths.
func writeImages(stem string, images [][]byte) ([]string, error) {
	names := outputNames(stem, len(images))
	for i, img := range images {
		if err := os.WriteFile(names[i], img, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", names[i], err)
		}
	}
	return names, nil
}
