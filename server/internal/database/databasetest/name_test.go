package databasetest

import "testing"

// The template name is cluster-wide but the advisory lock that guards its build only covers the
// database the process connects to, so two base databases on one cluster need their own templates.
func TestTemplateNameDependsOnTheBaseDatabaseAndRole(t *testing.T) {
	base, err := templateName("colliedesk", "colliedesk_test")
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range [][2]string{{"colliedesk", "colliedesk_test_b"}, {"other_role", "colliedesk_test"}} {
		name, err := templateName(other[0], other[1])
		if err != nil {
			t.Fatal(err)
		}
		if name == base {
			t.Fatalf("role %q database %q share the template %s of colliedesk/colliedesk_test", other[0], other[1], name)
		}
	}
	if again, _ := templateName("colliedesk", "colliedesk_test"); again != base {
		t.Fatalf("template name is not stable: %s then %s", base, again)
	}
}
