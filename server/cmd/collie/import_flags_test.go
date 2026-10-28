package main

import (
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

func TestHelpCenterPortalPicksThePortalWhoseArticlesStayPublished(t *testing.T) {
	t.Setenv("FRESHDESK_API_KEY", "key")
	base := []string{"--workspace", "acme", "--domain", "acme.freshdesk.com"}
	off, _, err := parseImportFlags(base)
	if err != nil || off.HelpCenterPortal != 0 {
		t.Fatalf("default options = %+v, %v", off, err)
	}
	on, _, err := parseImportFlags(append(base, "--help-center-portal", "5"))
	if err != nil || on.HelpCenterPortal != 5 {
		t.Fatalf("with the flag = %+v, %v", on, err)
	}
	if _, _, err := parseImportFlags(append(base, "--help-center-portal", "-1")); err == nil {
		t.Fatal("a negative portal id was accepted")
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
