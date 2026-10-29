// Package registry lists the channel types this server runs. It is the one place that imports the
// type packages: adding a type is one line here (docs/internals/channels.md).
package registry

import (
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channelemail"
	"github.com/colliedesk/colliedesk/server/internal/channels/helpchat"
	"github.com/colliedesk/colliedesk/server/internal/channels/webform"
)

// Config is what the types need of the server.
type Config struct {
	Email    channelemail.Config
	HelpChat helpchat.Config
}

// New returns the registry of every type, in the order the web app lists them.
func New(config Config) *channels.Registry {
	registry, err := channels.NewRegistry(
		channelemail.New(config.Email),
		webform.New(),
		helpchat.New(config.HelpChat),
	)
	if err != nil {
		panic(err) // a programming error: the list above is fixed
	}
	return registry
}
