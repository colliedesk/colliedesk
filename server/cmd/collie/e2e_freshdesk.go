//go:build e2e

package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// End-to-end builds point the importer at a local fake Freshdesk API. The fake
// is the only external boundary replaced: the command, importer, database and
// attachment storage are the real ones.
//
//	COLLIEDESK_E2E_FRESHDESK_URL   https://127.0.0.1:<port> (account API root)
//	COLLIEDESK_E2E_FRESHDESK_CA    PEM file with the fake's certificate
//	COLLIEDESK_E2E_FRESHDESK_DIAL  comma-separated host:port the importer may dial
func init() {
	base := os.Getenv("COLLIEDESK_E2E_FRESHDESK_URL")
	if base == "" {
		return
	}
	freshdeskBaseURL = func(string) string { return base }
	importRun = withE2ENetwork(importRun)
	importJobNetwork = func(options *freshdesk.Options) {
		pem, err := os.ReadFile(os.Getenv("COLLIEDESK_E2E_FRESHDESK_CA"))
		roots := x509.NewCertPool()
		if err != nil || !roots.AppendCertsFromPEM(pem) {
			return // the run then refuses the loopback address and says so
		}
		freshdesk.UseE2ENetwork(options, roots, strings.Split(os.Getenv("COLLIEDESK_E2E_FRESHDESK_DIAL"), ",")...)
	}
}

// withE2ENetwork wraps the importer entry point; the pool passes through untouched (its type is
// inferred, so this file never handles a raw database pool).
func withE2ENetwork[Pool any](next func(context.Context, Pool, freshdesk.Options) (*freshdesk.Report, error)) func(context.Context, Pool, freshdesk.Options) (*freshdesk.Report, error) {
	return func(ctx context.Context, pool Pool, options freshdesk.Options) (*freshdesk.Report, error) {
		pem, err := os.ReadFile(os.Getenv("COLLIEDESK_E2E_FRESHDESK_CA"))
		if err != nil {
			return nil, fmt.Errorf("read e2e Freshdesk CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("e2e Freshdesk CA holds no certificate")
		}
		freshdesk.UseE2ENetwork(&options, roots, strings.Split(os.Getenv("COLLIEDESK_E2E_FRESHDESK_DIAL"), ",")...)
		return next(ctx, pool, options)
	}
}
