package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"

	// "log"
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

// UploadImageToImgBB uploads image bytes to imgbb.com and returns the URL
func UploadImageToImgBB(data []byte, filename string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add required form fields for imgbb
	if err := writer.WriteField("type", "file"); err != nil {
		return "", fmt.Errorf("writing type field: %w", err)
	}

	if err := writer.WriteField("action", "upload"); err != nil {
		return "", fmt.Errorf("writing action field: %w", err)
	}

	// Add timestamp (current time in milliseconds)
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	if err := writer.WriteField("timestamp", timestamp); err != nil {
		return "", fmt.Errorf("writing timestamp field: %w", err)
	}

	// Add auth_token - this is a static token for anonymous uploads
	// This token is typically found in the imgbb page source
	if err := writer.WriteField("auth_token", "a416004be96cc2a6ddac10b2f4fc72e900ffd48f"); err != nil {
		return "", fmt.Errorf("writing auth_token field: %w", err)
	}

	// Add the file
	part, err := writer.CreateFormFile("source", filename)
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}

	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("writing data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("closing writer: %w", err)
	}

	req, err := http.NewRequest("POST", "https://imgbb.com/json", body)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://imgbb.com")
	req.Header.Set("Referer", "https://imgbb.com/upload")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("uploading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("imgbb returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// ImgBB returns JSON with nested structure
	var result struct {
		StatusCode int    `json:"status_code"`
		StatusTxt  string `json:"status_txt"`
		Image      struct {
			URL        string `json:"url"`         // Page URL
			DisplayURL string `json:"display_url"` // Direct image URL
		} `json:"image"`
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	log.Printf("[DEBUG] ImgBB response: %s", string(bodyBytes))

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if result.StatusCode != 200 {
		return "", fmt.Errorf("imgbb error: %s", result.StatusTxt)
	}

	// Prefer display_url (direct image) over url (page)
	imageURL := result.Image.DisplayURL
	if imageURL == "" {
		imageURL = result.Image.URL
	}

	if imageURL == "" {
		return "", fmt.Errorf("no image URL in response")
	}

	log.Printf("[DEBUG] ImgBB returned URL: %s (display_url: %s, url: %s)", imageURL, result.Image.DisplayURL, result.Image.URL)

	return imageURL, nil
}
