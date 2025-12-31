package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func HttpGetJSON(url string, target interface{}) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 status code: %d for URL %s", resp.StatusCode, url)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// HttpGetBytes fetches raw bytes from a URL (useful for downloading images)
func HttpGetBytes(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d for URL %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

// UploadImage uploads image bytes to catbox.moe and returns the URL
func UploadImage(data []byte, filename string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add reqtype field required by catbox
	if err := writer.WriteField("reqtype", "fileupload"); err != nil {
		return "", fmt.Errorf("writing reqtype: %w", err)
	}

	part, err := writer.CreateFormFile("fileToUpload", filename)
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}

	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("writing data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("closing writer: %w", err)
	}

	req, err := http.NewRequest("POST", "https://catbox.moe/user/api.php", body)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("uploading: %w", err)
	}
	defer resp.Body.Close()

	// Catbox returns the URL directly as plain text
	urlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	url := strings.TrimSpace(string(urlBytes))
	if !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("catbox error: %s", url)
	}

	return url, nil
}
