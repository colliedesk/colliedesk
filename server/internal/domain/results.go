package domain

import "errors"

// ErrResultsRefreshed: a number's list names a results snapshot that is no longer kept (or never was in
// this workspace); the page has newer numbers.
var ErrResultsRefreshed = errors.New("these numbers were refreshed")
