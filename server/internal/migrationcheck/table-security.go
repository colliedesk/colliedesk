package migrationcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tableSecurityHelpers is the migration that added secure_workspace_table, secure_server_table and
// limit_app_role. Migrations after it call those instead of writing row-level security by hand.
const tableSecurityHelpers = "20261121132937-workspace-table-security"

var (
	handWrittenTableSecurity = regexp.MustCompile(`(?i)create\s+policy|row\s+level\s+security|pg_policies|app\.privileged`)
	sqlLineComment           = regexp.MustCompile(`--[^\n]*`)
)

func checkTableSecurity(dir, stem string, files map[string]string) error {
	if stem <= tableSecurityHelpers {
		return nil
	}
	for _, name := range files {
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		code := sqlLineComment.ReplaceAllString(string(contents), "")
		if match := handWrittenTableSecurity.FindString(code); match != "" {
			return fmt.Errorf("migration %q writes %q by hand: use SELECT secure_workspace_table('t'), "+
				"secure_server_table('t') or limit_app_role('t', ...) (docs/internals/workspace-isolation.md)",
				name, strings.ToUpper(match))
		}
	}
	return nil
}
