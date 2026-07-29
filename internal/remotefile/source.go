// Package remotefile defines the protocol-independent remote file read boundary.
package remotefile

import (
	"context"
	"io"
	"time"
)

type Info struct {
	Size     int64
	Modified time.Time
	Version  string
}
type Source interface {
	Stat(context.Context, string) (Info, error)
	OpenRange(context.Context, string, int64, int64) (io.ReadCloser, error)
}
