// Package metaconversions tells Meta's Conversions API that a Colliedesk Cloud sign-up from a Meta ad
// started a trial (StartTrial) and later a paid plan (Subscribe), so Meta can measure and target its ads
// by trials instead of clicks (docs/internals/ad-conversions.md). Only the hosted cloud reports: a
// self-hosted server has no setting that turns it on.
package metaconversions

import "strings"

// GraphVersion is the Graph API version the events go to: the newest on 2026-11-01 (v26.0, released
// 2026-07-29). Meta keeps a version for about two years; move it forward before it expires.
const GraphVersion = "v26.0"

// graphBase is Meta's Graph API.
const graphBase = "https://graph.facebook.com"

// The environment variables. They stay out of the server's configuration in the database on purpose: a
// key there would be a setting in every self-hosted operator console.
const (
	EnvDatasetID     = "META_CONVERSIONS_DATASET_ID"
	EnvAccessToken   = "META_CONVERSIONS_ACCESS_TOKEN"
	EnvTestEventCode = "META_CONVERSIONS_TEST_EVENT_CODE"
)

// Config says where the events go. The zero Config reports nothing.
type Config struct {
	// DatasetID is the Meta dataset (formerly pixel) that receives the events.
	DatasetID string
	// AccessToken is the Conversions API token Events Manager generated for the dataset.
	AccessToken string
	// TestEventCode, when set, sends every event to Events Manager's "Test events" instead of the real
	// ones: for checking the setup, then unset.
	TestEventCode string
	// Base overrides the Graph API address (tests).
	Base string
}

// Enabled reports whether events are recorded and sent.
func (c Config) Enabled() bool { return c.DatasetID != "" && c.AccessToken != "" }

// ConfigFromEnv reads the variables on the hosted cloud only (cloud sign-in with billing). Everywhere
// else the result is off whatever the environment holds.
func ConfigFromEnv(getenv func(string) string, hostedCloud bool) Config {
	if !hostedCloud {
		return Config{}
	}
	return Config{
		DatasetID:     strings.TrimSpace(getenv(EnvDatasetID)),
		AccessToken:   strings.TrimSpace(getenv(EnvAccessToken)),
		TestEventCode: strings.TrimSpace(getenv(EnvTestEventCode)),
	}
}

func (c Config) endpoint() string {
	base := c.Base
	if base == "" {
		base = graphBase
	}
	return strings.TrimRight(base, "/") + "/" + GraphVersion + "/" + c.DatasetID + "/events"
}
