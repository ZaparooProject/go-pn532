//go:build !linux

package i2c

import (
	"context"

	"github.com/ZaparooProject/go-pn532/detection"
)

// detectLinux is a stub for non-Linux platforms
func detectLinux(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	return nil, detection.ErrUnsupportedPlatform
}
