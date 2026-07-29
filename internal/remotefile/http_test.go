package remotefile

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSourceUsesEtaReadAPI(t *testing.T) {
	modified := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/list" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"entry":{"kind":"file","size":3,"modified":"`+modified.Format(time.RFC3339Nano)+`"}}`)
			return
		}
		if r.Header.Get("Range") != "bytes=1-2" {
			t.Errorf("range=%q", r.Header.Get("Range"))
		}
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, "bc")
	}))
	defer server.Close()
	s := &HTTPSource{BaseURL: server.URL, Root: 2}
	info, e := s.Stat(context.Background(), "a.txt")
	if e != nil || info.Size != 3 {
		t.Fatal(e)
	}
	r, e := s.OpenRange(context.Background(), "a.txt", 1, 2)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(r)
	r.Close()
	if e != nil || string(b) != "bc" {
		t.Fatal(e)
	}
}
