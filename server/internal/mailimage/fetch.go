package mailimage

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/netguard"
)

const (
	// DefaultMaxBytes bounds one image; it is the size the email channel and the importer allow too.
	DefaultMaxBytes = 5 << 20
	// maxRedirects is how many redirects one fetch follows; each hop is dialled through the guard.
	maxRedirects   = 3
	defaultTimeout = 10 * time.Second
	// userAgent is a plain browser-like string: it names neither this product nor the viewer.
	userAgent = "Mozilla/5.0 (compatible; ImageFetcher/1.0)"
)

var (
	// ErrNotImage means the bytes are not a PNG, JPEG, GIF or WebP image (SVG included).
	ErrNotImage = errors.New("the response is not a PNG, JPEG, GIF or WebP image")
	// ErrTooLarge means the image is over the size cap.
	ErrTooLarge = errors.New("the image is over the size limit")
)

// Options configure a Fetcher. The zero value is the production setting.
type Options struct {
	// MaxBytes caps one image (default DefaultMaxBytes); Timeout caps the whole fetch (default 10 s).
	MaxBytes int64
	Timeout  time.Duration
	// AllowAddresses lists "host:port" dial targets that bypass the address guard. Tests use it for a
	// local server; the server never sets it.
	AllowAddresses map[string]bool
}

// Image is a fetched raster image. ContentType comes from its bytes, never from the source's header.
type Image struct {
	Data        []byte
	ContentType string
}

// Fetcher downloads remote images for the proxy. It dials only public addresses, checked on every
// connection (so DNS rebinding and redirects are covered), follows at most three redirects, sends no
// cookies, credentials or Referer, and keeps raster images under the size cap.
type Fetcher struct {
	client   *http.Client
	maxBytes int64
	timeout  time.Duration
}

func NewFetcher(options Options) *Fetcher {
	f := &Fetcher{maxBytes: options.MaxBytes, timeout: options.Timeout}
	if f.maxBytes <= 0 {
		f.maxBytes = DefaultMaxBytes
	}
	if f.timeout <= 0 {
		f.timeout = defaultTimeout
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: netguard.Control(options.AllowAddresses)}
	f.client = &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: f.timeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("more than %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to %s is not allowed", req.URL.Scheme)
			}
			req.Header.Del("Referer")
			return nil
		},
	}
	return f
}

// Fetch downloads one image. The error wraps netguard.ErrBlockedAddress, ErrNotImage or ErrTooLarge
// when that was the reason, so callers can answer precisely.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Image, error) {
	u, ok := ParseSrc(rawURL)
	if !ok {
		return Image{}, errors.New("the address is not an http or https URL")
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp,*/*;q=0.1")
	response, err := f.client.Do(request)
	if err != nil {
		return Image{}, fmt.Errorf("fetch image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("fetch image: the source answered %d", response.StatusCode)
	}
	if response.ContentLength > f.maxBytes {
		return Image{}, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, f.maxBytes+1))
	switch {
	case err != nil:
		return Image{}, fmt.Errorf("read image: %w", err)
	case int64(len(data)) > f.maxBytes:
		return Image{}, ErrTooLarge
	}
	contentType := SniffRaster(data)
	if contentType == "" {
		return Image{}, ErrNotImage
	}
	return Image{Data: data, ContentType: contentType}, nil
}
