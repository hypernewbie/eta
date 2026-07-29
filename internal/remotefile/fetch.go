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
func ReadRange(ctx context.Context, source Source, path string, offset, length int64) ([]byte, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("invalid range")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := source.OpenRange(ctx, path, offset, length)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

func readRange(ctx context.Context, source Source, path string, size int64) ([]byte, error) {
	return ReadRange(ctx, source, path, 0, size)
}
