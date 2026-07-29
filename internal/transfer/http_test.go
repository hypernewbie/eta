package transfer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendFilePostsOnlyMissingVerifiedChunks(t *testing.T) {
	var got []byte
	finalized := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/transfers":
			json.NewEncoder(w).Encode(map[string]string{"id": "x"})
		case r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"missing": []int{0}})
		case r.Method == "PUT":
			got, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			finalized = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	source := filepath.Join(t.TempDir(), "a")
	if e := os.WriteFile(source, []byte("eta"), 0600); e != nil {
		t.Fatal(e)
	}
	id, e := SendFile(context.Background(), srv.Client(), srv.URL, 0, "dest", source)
	if e != nil || id != "x" || string(got) != "eta" || !finalized {
		t.Fatalf("id=%q got=%q final=%v err=%v", id, got, finalized, e)
	}
}
