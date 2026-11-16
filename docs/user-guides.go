// Package docs holds the user guides in docs/user, embedded so the server's documentation search
// reads the files people edit, with no copy to keep in sync.
package docs

import "embed"

// UserGuides holds docs/user/*.md under the directory "user".
//
//go:embed user/*.md
var UserGuides embed.FS
