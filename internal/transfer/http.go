package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// SendFile transfers source directly to an Eta peer, resuming only its missing
// verified chunks. The coordinator need not proxy file bytes.
func SendFile(ctx context.Context, client *http.Client, baseURL string, root int, destination, source string) (string, error) {
	return SendFileWithProgress(ctx, client, baseURL, root, destination, source, nil)
}

// SendFileWithProgress reports each peer-acknowledged chunk. A nil callback
// leaves the protocol behavior identical to SendFile.
func SendFileWithProgress(ctx context.Context, client *http.Client, baseURL string, root int, destination, source string, progress func(completed, total int)) (string, error) {
	file, err := os.Open(source)
	if err != nil {
		return "", err
	}
	manifest, err := BuildManifest(file, DefaultChunkSize)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/transfers"
	payload, err := json.Marshal(map[string]any{"root": root, "path": destination, "manifest": manifest})
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"id"`
	}
	if err = requestJSON(ctx, client, http.MethodPost, endpoint, bytes.NewReader(payload), &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("peer did not return transfer ID")
	}
	var status struct {
		Missing []int `json:"missing"`
	}
	if err = requestJSON(ctx, client, http.MethodGet, endpoint+"/"+created.ID, nil, &status); err != nil {
		return "", err
	}
	file, err = os.Open(source)
	if err != nil {
		return "", err
	}
	defer file.Close()
	completed := len(manifest.Chunks) - len(status.Missing)
	if progress != nil {
		progress(completed, len(manifest.Chunks))
	}
	for _, index := range status.Missing {
		length, err := manifest.ChunkLength(index)
		if err != nil {
			return "", err
		}
		body := make([]byte, length)
		if _, err = file.ReadAt(body, int64(index)*manifest.ChunkSize); err != nil {
			return "", err
		}
		if err = requestJSON(ctx, client, http.MethodPut, fmt.Sprintf("%s/%s/chunks/%d", endpoint, created.ID, index), bytes.NewReader(body), nil); err != nil {
			return "", err
		}
		completed++
		if progress != nil {
			progress(completed, len(manifest.Chunks))
		}
	}
	finish, _ := json.Marshal(map[string]any{"root": root, "path": destination})
	if err = requestJSON(ctx, client, http.MethodPost, endpoint+"/"+created.ID+"/finalize", bytes.NewReader(finish), nil); err != nil {
		return "", err
	}
	return created.ID, nil
}
func requestJSON(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, result any) error {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("transfer peer: %s", response.Status)
	}
	if result != nil {
		return json.NewDecoder(response.Body).Decode(result)
	}
	return nil
}
func SourceName(path string) string { return filepath.Base(path) }
