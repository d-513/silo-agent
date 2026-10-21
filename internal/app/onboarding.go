package app

import (
	"strings"

	"silo.agent/internal/app/run"
	"silo.agent/internal/prompts"
)

// onboardingPrompt is the interview: the operator's own, else the built-in.
func (a *App) onboardingPrompt() string {
	if p := strings.TrimSpace(a.cfg().Onboarding.Prompt); p != "" {
		return p
	}
	return strings.TrimSpace(prompts.Onboarding)
}

// startInterview opens a chat with the interview, only while no reply runs
// there (run.ErrBusy otherwise). Its opening turn is Silo's, marked
// run.FromOnboarding, and the thread draws it as a divider.
func (a *App) startInterview(botID, chatID string) (string, error) {
	return a.StartIfIdle(run.Request{BotID: botID, ChatID: chatID, Text: a.onboardingPrompt(), From: run.FromOnboarding})
}
