//go:build e2e

package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5/pgxpool"
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
	next := importRun
	importRun = func(ctx context.Context, pool *pgxpool.Pool, options freshdesk.Options) (*freshdesk.Report, error) {
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
