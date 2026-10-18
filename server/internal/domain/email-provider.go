package domain

import "time"

// ProviderCheckMinInterval is the shortest time between two domain checks of one workspace that reach the
// email provider. The provider account and its request limit are shared by every workspace, so one
// workspace cannot spend them by clicking Verify.
const ProviderCheckMinInterval = 30 * time.Second

// ProviderDomainUnverifiedTTL is how long a provider domain may stay unverified. After that it is deleted at
// the provider and the workspace verifies again to get a new one, so abandoned attempts do not hold the
// provider account's limited domain slots.
const ProviderDomainUnverifiedTTL = 7 * 24 * time.Hour
