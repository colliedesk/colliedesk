package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// mailImageCacheBytesFromEnv reads MAIL_IMAGE_CACHE_BYTES, the byte cap of the email image cache (default
// 1 GiB). The cache holds only copies of remote images and is evicted under pressure; it does not count
// against a plan's storage.
func mailImageCacheBytesFromEnv(getenv func(string) string) (int64, error) {
	value := strings.TrimSpace(getenv("MAIL_IMAGE_CACHE_BYTES"))
	if value == "" {
		return httpapi.DefaultMailImageCacheBytes, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("MAIL_IMAGE_CACHE_BYTES must be a whole number of bytes above zero")
	}
	return size, nil
}

// sweepMailImageCache keeps the email image cache under its cap and removes images older than their
// lifetime, at start and then hourly until ctx ends.
func sweepMailImageCache(ctx context.Context, data *store.Store, maxBytes int64) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if removed, err := data.SweepMailImageCache(ctx, httpapi.MailImageLifetime, maxBytes); err != nil && ctx.Err() == nil {
			slog.Error("sweep the email image cache", "error", err)
		} else if removed > 0 {
			slog.Info("evicted cached email images", "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// kbVideoMaxBytesFromEnv reads KB_VIDEO_MAX_BYTES, the largest knowledge base video a self-hosted
// server takes; unset is domain.MaxKBVideoBytes (200 MiB).
func kbVideoMaxBytesFromEnv(getenv func(string) string) (int64, error) {
	value := strings.TrimSpace(getenv("KB_VIDEO_MAX_BYTES"))
	if value == "" {
		return domain.MaxKBVideoBytes, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("KB_VIDEO_MAX_BYTES must be a whole number of bytes above zero")
	}
	return size, nil
}
