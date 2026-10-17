package main

import "testing"

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
