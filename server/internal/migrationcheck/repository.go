package migrationcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func RepositoryRoot(path string) (string, error) {
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve repository path: %w", err)
		}
		return absolute, nil
	}
	var err error
	path, err = os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = absolute
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Git repository root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
