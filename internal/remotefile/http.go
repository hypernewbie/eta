package remotefile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPSource adapts another Eta instance's existing read-only API to Source.
// Peer discovery/configuration remains separate from this transport boundary.
type HTTPSource struct {
	BaseURL string
	Root    int
	Client  *http.Client
}

func (s *HTTPSource) Stat(ctx context.Context, path string) (Info, error) {
	endpoint, err := s.endpoint("/api/list", path)
	if err != nil {
		return Info{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Info{}, err
	}
	response, err := s.client().Do(request)
	if err != nil {
		return Info{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("peer stat: %s", response.Status)
	}
	var result struct {
		Entry struct {
			Kind     string    `json:"kind"`
			Size     int64     `json:"size"`
			Modified time.Time `json:"modified"`
		} `json:"entry"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return Info{}, err
	}
	if result.Entry.Kind != "file" {
		return Info{}, fmt.Errorf("peer source is not a file")
	}
	return Info{Size: result.Entry.Size, Modified: result.Entry.Modified, Version: strconv.FormatInt(result.Entry.Size, 10) + ":" + strconv.FormatInt(result.Entry.Modified.UnixNano(), 10)}, nil
}
func (s *HTTPSource) OpenRange(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length <= 0 {
		return nil, fmt.Errorf("invalid range")
	}
	endpoint, err := s.endpoint("/api/file", path)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	response, err := s.client().Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusPartialContent && response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("peer range: %s", response.Status)
	}
	return response.Body, nil
}
func (s *HTTPSource) endpoint(route, path string) (string, error) {
	base, err := url.Parse(s.BaseURL)
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + route
	q := base.Query()
	q.Set("root", strconv.Itoa(s.Root))
	q.Set("path", path)
	base.RawQuery = q.Encode()
	return base.String(), nil
}
func (s *HTTPSource) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}
