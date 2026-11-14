package domain

import "testing"

func TestCleanMacroRefusesABlankMacroAndCleansActions(t *testing.T) {
	if _, _, _, _, err := CleanMacro("  ", "Hello", MacroShared, MacroActions{}); err == nil {
		t.Fatal("a blank title was accepted")
	}
	if _, _, _, _, err := CleanMacro("Close", "  ", MacroShared, MacroActions{}); err == nil {
		t.Fatal("a macro with no text and no action was accepted")
	}
	title, body, scope, actions, err := CleanMacro(" Close ", "Thanks {{customer.name}}\n", MacroPersonal, MacroActions{
		Status: strPtr(" PENDING "), AddTagIDs: []string{"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if title != "Close" || body != "Thanks {{customer.name}}" || scope != MacroPersonal || actions.Status == nil || *actions.Status != "pending" || len(actions.AddTagIDs) != 1 {
		t.Fatalf("cleaned = %q %q %s %+v", title, body, scope, actions)
	}
	if _, _, _, _, err := CleanMacro("Both", "Text", MacroShared, MacroActions{
		AddTagIDs: []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, RemoveTagIDs: []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}); err == nil {
		t.Fatal("adding and removing the same tag was accepted")
	}
}

func TestRenderMacroFillsTheNamesAndDropsAnUnknownOne(t *testing.T) {
	got := RenderMacro("Hi {{ customer.name }}, {{ticket.id}} from {{agent.name}}. {{requester.name}} {{missing}}", MacroContext{
		CustomerName: "Mai", AgentName: "Ada", TicketID: "#12",
	})
	if got != "Hi Mai, #12 from Ada. Mai {{missing}}" {
		t.Fatalf("rendered = %q", got)
	}
	if got := RenderMacro("Hi {{customer.name}}, thanks", MacroContext{}); got != "Hi, thanks" {
		t.Fatalf("empty name = %q", got)
	}
}

func TestAgentSignatureNameUsesTheEmailWhenTheDisplayNameIsBlank(t *testing.T) {
	if got := AgentSignatureName("  Ada  ", "ada@example.com"); got != "Ada" {
		t.Fatalf("display name = %q", got)
	}
	if got := AgentSignatureName("", "ada+desk@example.com"); got != "ada" {
		t.Fatalf("email = %q", got)
	}
}

func strPtr(value string) *string { return &value }
