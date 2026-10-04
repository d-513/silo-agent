package app

import "silo.agent/internal/app/feed"

// The domain packages each serve their own slice of the UI service. A name per
// service keeps the embedded fields of uiHandler distinct.
type feedRPC = feed.Service

// uiHandler is the whole UI service: App's own RPCs plus the ones the domain
// packages implement, promoted by embedding.
type uiHandler struct {
	*App
	*feedRPC
}

func (a *App) uiHandler() *uiHandler {
	return &uiHandler{App: a, feedRPC: a.feed}
}
