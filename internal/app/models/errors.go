package models

import "errors"

// ErrNoQuery is a search with nothing to search for.
var ErrNoQuery = errors.New("query required")

// ProviderError marks a failure that should leave a background job's progress
// in place and pause its sweep: the model or the embedder could not be reached.
type ProviderError struct{ Err error }

func (e ProviderError) Error() string { return e.Err.Error() }
func (e ProviderError) Unwrap() error { return e.Err }
