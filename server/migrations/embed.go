package migrations

import "embed"

// Files contains the ordered SQL migration catalog.
//
//go:embed *.sql
var Files embed.FS
