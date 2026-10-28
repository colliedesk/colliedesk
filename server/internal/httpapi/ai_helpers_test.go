package httpapi_test

import (
	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

// fakeAIConfig makes every AI provider the fake: no test reaches a real provider.
func fakeAIConfig(fake *aiprovider.Fake) ai.Config {
	return ai.Config{Build: func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }}
}
