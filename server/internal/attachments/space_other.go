//go:build !unix

package attachments

import "errors"

func availableBytes(string) (int64, error) {
	return 0, errors.New("free disk space is not available on this platform")
}
