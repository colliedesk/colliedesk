package store

import "strings"

func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
