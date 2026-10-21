package domain

import "slices"

// The answers a cloud sign-up may give; the lists match contracts/openapi.yaml and the CHECK
// constraints of workspace_onboarding.
var (
	OnboardingGoals   = []string{"leave_helpdesk", "shared_inbox", "lower_cost", "ai_agents", "other"}
	OnboardingRoles   = []string{"founder", "support_lead", "support_agent", "developer", "other"}
	OnboardingSources = []string{"x", "email", "search", "reddit", "github", "referral", "other"}
)

// Onboarding holds what a person said about their team at cloud sign-up. A nil field was not answered.
type Onboarding struct {
	Goal   *string
	Role   *string
	Source *string
}

// Valid reports whether every given answer is one of its listed values.
func (o Onboarding) Valid() bool {
	return validAnswer(o.Goal, OnboardingGoals) && validAnswer(o.Role, OnboardingRoles) && validAnswer(o.Source, OnboardingSources)
}

// Properties returns the given answers by name, for the trial_started funnel event; nil when none were given.
func (o Onboarding) Properties() map[string]any {
	var properties map[string]any
	for name, value := range map[string]*string{"goal": o.Goal, "role": o.Role, "source": o.Source} {
		if value == nil {
			continue
		}
		if properties == nil {
			properties = map[string]any{}
		}
		properties[name] = *value
	}
	return properties
}

func validAnswer(value *string, allowed []string) bool {
	return value == nil || slices.Contains(allowed, *value)
}
