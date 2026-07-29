package remotefile

import (
	"bytes"
	"context"
	"errors"
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
func (f *fake) OpenRange(_ context.Context, _ string, offset, length int64) (io.ReadCloser, error) {
	f.opens++
	return io.NopCloser(bytes.NewReader(f.body[offset : offset+length])), nil
}
func TestReadRangeHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadRange(ctx, &fake{body: []byte("eta")}, "a", 0, 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

func TestReadCachedRangeCachesVersionedBlock(t *testing.T) {
	c, err := diskcache.New(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	s := &fake{body: []byte("eta remote")}
	for range 2 {
		body, _, err := ReadCachedRange(context.Background(), c, s, "a", 4, 6)
		if err != nil || string(body) != "remote" {
			t.Fatalf("body=%q err=%v", body, err)
		}
	}
	if s.opens != 1 {
		t.Fatalf("opens=%d", s.opens)
	}
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
