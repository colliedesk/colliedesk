package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPersonalCannedResponsesAreImportedOnlyWithTheFlag(t *testing.T) {
	t.Setenv("FRESHDESK_API_KEY", "key")
	base := []string{"--workspace", "acme", "--domain", "acme.freshdesk.com"}
	off, _, err := parseImportFlags(base)
	if err != nil || off.IncludePersonalCannedResponses {
		t.Fatalf("default options = %+v, %v", off, err)
	}
	on, _, err := parseImportFlags(append(base, "--include-personal-canned-responses"))
	if err != nil || !on.IncludePersonalCannedResponses {
		t.Fatalf("with the flag = %+v, %v", on, err)
	}
}

func TestHelpCenterPortalIsAcceptedAndNoLongerNeeded(t *testing.T) {
	t.Setenv("FRESHDESK_API_KEY", "key")
	var warnings bytes.Buffer
	previous := progressOutput
	progressOutput = &warnings
	t.Cleanup(func() { progressOutput = previous })
	base := []string{"--workspace", "acme", "--domain", "acme.freshdesk.com"}
	if _, _, err := parseImportFlags(base); err != nil || warnings.Len() != 0 {
		t.Fatalf("without the flag: %v, warned %q", err, warnings.String())
	}
	if _, _, err := parseImportFlags(append(base, "--help-center-portal", "5")); err != nil {
		t.Fatalf("an earlier script's --help-center-portal was refused: %v", err)
	}
	if got := warnings.String(); !strings.Contains(got, "--help-center-portal is no longer needed: every portal becomes its own help center") {
		t.Fatalf("warning = %q", got)
	}
}

func TestOldHelpCenterDomainNamesTheOldPortalsOwnHost(t *testing.T) {
	t.Setenv("FRESHDESK_API_KEY", "key")
	base := []string{"--workspace", "acme", "--domain", "acme.freshdesk.com"}
	off, _, err := parseImportFlags(base)
	if err != nil || off.OldHelpCenterDomain != "" {
		t.Fatalf("default options = %+v, %v", off, err)
	}
	on, _, err := parseImportFlags(append(base, "--old-help-center-domain", "https://Support.Acme.com/"))
	if err != nil || on.OldHelpCenterDomain != "support.acme.com" {
		t.Fatalf("with the flag = %q, %v", on.OldHelpCenterDomain, err)
	}
	if _, _, err := parseImportFlags(append(base, "--old-help-center-domain", "support.acme.com/support/home")); err == nil || !strings.Contains(err.Error(), "--old-help-center-domain") {
		t.Fatalf("a path was accepted as the old help center domain: %v", err)
	}
}
