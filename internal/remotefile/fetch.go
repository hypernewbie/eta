package remotefile

import (
	"context"
	"fmt"
	"io"

	"github.com/hypernewbie/eta/internal/diskcache"
)

// Fetch reads a versioned remote file through a cache. It deliberately has no
// peer protocol dependency: transports implement Source and tests use fakes.
func Fetch(ctx context.Context, cache *diskcache.Cache, source Source, path string) ([]byte, Info, error) {
	info, err := source.Stat(ctx, path)
	if err != nil {
		return nil, Info{}, err
	}
	if info.Size < 0 {
		return nil, Info{}, fmt.Errorf("negative remote size")
	}
	key := info.Version + "\x00" + path
	if cache != nil {
		if body, ok, err := cache.Get(key); err != nil {
			return nil, Info{}, err
		} else if ok {
			return body, info, nil
		}
	}
	body, err := readRange(ctx, source, path, info.Size)
	if err != nil {
		return nil, Info{}, err
	}
	if cache != nil {
		if err := cache.Put(key, body); err != nil {
			return nil, Info{}, err
		}
	}
	return body, info, nil
}
func readRange(ctx context.Context, s Source, path string, size int64) ([]byte, error) {
	r, e := s.OpenRange(ctx, path, 0, size)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	return io.ReadAll(r)
}
