package migrationcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// tableSecurityHelpers defines secure_workspace_table, secure_server_table and limit_app_role, and
// rowLevelSecurity sets row-level security up with them; helpCenterRowSecurity defines
// secure_help_center_table and the help center reader role; helpCenterTableRelease defines
// release_help_center_table, its undo for a down migration. The four spell it out by hand. Every
// other up migration calls the helpers. A down migration up to oldDownMigrations may still undo row-level
// security by hand, as it did when it was applied.
const (
	tableSecurityHelpers   = "20261014135949-table-security-helpers"
	rowLevelSecurity       = "20261014135950-workspace-row-level-security"
	oldDownMigrations      = "20261121132937-workspace-table-security"
	helpCenterRowSecurity  = "20261122081247-help-center-row-security"
	helpCenterTableRelease = "20261122220740-help-center-table-release"
)

var handWrittenTableSecurity = regexp.MustCompile(
	`(?i)(create|alter|drop)\s+policy|row\s+level\s+security|pg_policies|app\.privileged`)

func checkTableSecurity(dir, stem string, files map[string]string) error {
	if stem == tableSecurityHelpers || stem == rowLevelSecurity || stem == helpCenterRowSecurity || stem == helpCenterTableRelease {
		return nil
	}
	for direction, name := range files {
		if direction == "down" && stem <= oldDownMigrations {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		code := withoutComments(string(contents))
		if match := handWrittenTableSecurity.FindString(code); match != "" {
			return fmt.Errorf("migration %q writes %q by hand: use SELECT secure_workspace_table('t'), "+
				"secure_server_table('t'), secure_help_center_table('t', ...), release_help_center_table('t') or limit_app_role('t', ...) "+
				"(docs/internals/workspace-isolation.md)",
				name, strings.ToUpper(match))
		}
	}
	return nil
}

// withoutComments drops -- and /* */ comments; quoted strings are kept, so a "--" inside one does not
// hide the statement after it.
func withoutComments(sql string) string {
	var code strings.Builder
	for i := 0; i < len(sql); i++ {
		switch {
		case sql[i] == '\'':
			end := strings.IndexByte(sql[i+1:], '\'')
			if end < 0 {
				return code.String() + sql[i:]
			}
			code.WriteString(sql[i : i+end+2])
			i += end + 1
		case strings.HasPrefix(sql[i:], "--"):
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				return code.String()
			}
			i += end - 1
		case strings.HasPrefix(sql[i:], "/*"):
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return code.String()
			}
			code.WriteByte(' ')
			i += end + 3
		default:
			code.WriteByte(sql[i])
		}
	}
	return code.String()
}
