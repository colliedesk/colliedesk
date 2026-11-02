package auth

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// agents.role, the role ceilings and the tier in sessions.scope stay for a rollback and are still
// written, but no code decides on them any more: what a call may do comes from roles only. A pairing
// link's role_ceiling is the one exception (it records that the link asked for the Agent role, design
// 1.3.5), and the operator console never had roles.
func TestNoCodeDecidesOnTheLegacyRoleColumns(t *testing.T) {
	reads := regexp.MustCompile(`(?i)(\brole\s*=\s*'admin'|\brole\s*=\s*'agent'|a\.role\b[^_]|Role\s*[!=]=\s*"admin"|Role\s*[!=]=\s*"agent"|HasPermission\(|role_ceiling\s*=\s*'agent'\s*(OR|AND|THEN)|CanDelegate\()`)
	allowed := map[string]bool{
		"store/roles.go": true, // syncLegacyRole writes agents.role
		"auth/links.go":  true, // a pairing link's ceiling asks for the Agent role
	}
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "mcpserver" || entry.Name() == "testdata") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative := strings.TrimPrefix(filepath.ToSlash(path), "../")
		if allowed[relative] || strings.HasPrefix(relative, "auth/operator") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for number, line := range strings.Split(string(data), "\n") {
			if reads.MatchString(line) {
				t.Errorf("%s:%d decides on a legacy role column: %s", relative, number+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
