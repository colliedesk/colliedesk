package domain

import "time"

// ProviderCheckMinInterval is the shortest time between two domain checks of one workspace that reach the
// email provider. The provider account and its request limit are shared by every workspace, so one
// workspace cannot spend them by clicking Verify.
const ProviderCheckMinInterval = 30 * time.Second
