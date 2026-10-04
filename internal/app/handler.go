package app

import (
	"silo.agent/internal/app/artifact"
	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/channel"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/app/drive"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/app/knowledge"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/app/models"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/skill"
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
	knowRPC   = knowledge.Service
	memRPC    = memory.Service
	driveRPC  = drive.Service
	autoRPC   = automation.Service
	chanRPC   = channel.Service
	connRPC   = connector.Service
	skillRPC  = skill.Service
	artRPC    = artifact.Service
)

// uiHandler is the whole UI service: App's own RPCs plus the ones the domain
// packages implement, promoted by embedding.
type uiHandler struct {
	*App
	*feedRPC
	*modelsRPC
	*filesRPC
	*voiceRPC
	*knowRPC
	*memRPC
	*driveRPC
	*autoRPC
	*chanRPC
	*connRPC
	*skillRPC
	*artRPC
}

func (a *App) uiHandler() *uiHandler {
	return &uiHandler{App: a, feedRPC: a.Feed, modelsRPC: a.Models, filesRPC: a.Workspace, voiceRPC: a.Voice, knowRPC: a.Knowledge, memRPC: a.Memory, driveRPC: a.Drives, autoRPC: a.Automations, chanRPC: a.Channels, connRPC: a.Connectors, skillRPC: a.Skills, artRPC: a.Artifacts}
}

// The App is the engine the satellite domains drive.
var _ run.Engine = (*App)(nil)
