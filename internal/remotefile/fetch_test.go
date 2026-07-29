package remotefile

import (
	"bytes"
	"context"
	"github.com/hypernewbie/eta/internal/diskcache"
	"io"
	"testing"
)

type fake struct {
	body  []byte
	opens int
}

func (f *fake) Stat(context.Context, string) (Info, error) {
	return Info{Size: int64(len(f.body)), Version: "v1"}, nil
}
func (f *fake) OpenRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	f.opens++
	return io.NopCloser(bytes.NewReader(f.body)), nil
}
func TestFetchCachesVersionedRemoteFile(t *testing.T) {
	c, e := diskcache.New(t.TempDir(), 1024)
	if e != nil {
		t.Fatal(e)
	}
	s := &fake{body: []byte("eta")}
	for i := 0; i < 2; i++ {
		b, _, e := Fetch(context.Background(), c, s, "a")
		if e != nil || string(b) != "eta" {
			t.Fatal(e)
		}
	}
	if s.opens != 1 {
		t.Fatalf("opens=%d", s.opens)
	}
}
