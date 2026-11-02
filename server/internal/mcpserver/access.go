package mcpserver

// This file is the one place that reads httpapi's route table and the caller's permissions, so a
// change there changes only this file.

import (
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// routeAccess is what each route needs (httpapi's table), read once.
var routeAccess = httpapi.RouteAccessTable()

// routeNeeds is every permission the routes need, sorted: a need row gives its permission, a
// byHandler row the least the handler takes, and body what the tool's own body needs beyond that; a
// route any signed-in caller may use adds nothing.
func routeNeeds(rest, body []string) []string {
	needs := []string{}
	for _, permission := range body {
		if !slices.Contains(needs, permission) {
			needs = append(needs, permission)
		}
	}
	for _, key := range rest {
		access := routeAccess[key]
		if access.Anyone || access.Permission == "" || slices.Contains(needs, access.Permission) {
			continue
		}
		needs = append(needs, access.Permission)
	}
	slices.Sort(needs)
	return needs
}

// needsLine closes a tool's description with what it needs, so a model knows before it calls.
func needsLine(needs []string) string {
	if len(needs) == 0 {
		return "\n\nNeeds: nothing beyond a valid token."
	}
	return "\n\nNeeds: " + strings.Join(needs, ", ") + "."
}

// toolOpen reports whether a caller holding these permissions can use the tool. Operator tools
// never open to a workspace caller: they are served on OperatorPath only.
func toolOpen(spec Spec, held []string) bool {
	if spec.Operator {
		return false
	}
	for _, need := range spec.Needs {
		if !slices.Contains(held, need) {
			return false
		}
	}
	return true
}
