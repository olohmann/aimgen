package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
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

	return decodeImageResponse(resp)
}

// edit posts one or more input images plus a prompt to the edits endpoint and
// returns the decoded image bytes for each result. An optional mask enables
// inpainting. It builds a multipart/form-data body mirroring the generation
// parameters (model, size, count, format) so behavior is consistent across modes.
func (c *client) edit(ctx context.Context, prompt string, imagePaths []string, maskPath string) ([][]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	fields := map[string]string{
		"prompt":             prompt,
		"model":              c.cfg.Model,
		"size":               c.cfg.Size,
		"n":                  strconv.Itoa(c.cfg.Count),
		"output_format":      c.cfg.Format,
		"output_compression": strconv.Itoa(c.cfg.Compression),
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return nil, err
		}
	}

	// Single image uses the "image" field; multiple images use "image[]".
	imageField := "image"
	if len(imagePaths) > 1 {
		imageField = "image[]"
	}
	for _, p := range imagePaths {
		if err := addFilePart(mw, imageField, p); err != nil {
			return nil, err
		}
	}
	if maskPath != "" {
		if err := addFilePart(mw, "mask", maskPath); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.cfg.Endpoint, "/") + c.cfg.EditPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return decodeImageResponse(resp)
}

// addFilePart streams a file into the multipart writer under the given field
// name, setting a content type inferred from the file extension.
func addFilePart(mw *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filepath.Base(path)))
	h.Set("Content-Type", contentTypeFor(path))
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}

// contentTypeFor returns the MIME type for a supported image extension,
// defaulting to application/octet-stream for unknown extensions.
func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// decodeImageResponse reads an image endpoint response, mapping non-2xx status
// to a structured apiErr and decoding each base64 image payload. It is shared by
// the generate and edit paths, which return the same response shape.
func decodeImageResponse(resp *http.Response) ([][]byte, error) {
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
