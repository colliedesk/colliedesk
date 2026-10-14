//go:build unix

package attachments

import (
	"fmt"
	"syscall"
)

func availableBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("read free disk space: %w", err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil //nolint:unconvert // field types differ by OS
}
