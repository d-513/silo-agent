package app

import (
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/app/workspace"
)

// The domain packages each serve their own slice of the UI service. A name per
// service keeps the embedded fields of uiHandler distinct.
type (
	feedRPC   = feed.Service
	modelsRPC = models.Service
	filesRPC  = workspace.Service
	voiceRPC  = voice.Service
)

// uiHandler is the whole UI service: App's own RPCs plus the ones the domain
// packages implement, promoted by embedding.
type uiHandler struct {
	*App
	*feedRPC
	*modelsRPC
	*filesRPC
	*voiceRPC
}

func (a *App) uiHandler() *uiHandler {
	return &uiHandler{App: a, feedRPC: a.feed, modelsRPC: a.models, filesRPC: a.ws, voiceRPC: a.voice}
}
