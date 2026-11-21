package run

import (
	"testing"

	"silo.agent/internal/db"
)

func TestRequestKind(t *testing.T) {
	au, sa, ch := &db.Automation{}, &db.Subagent{}, &db.Channel{}
	for _, c := range []struct {
		req           Request
		kind, trigger string
	}{
		{Request{}, KindChat, TriggerMessage},
		{Request{Extra: Extra{Spoken: &Spoken{}}}, KindChat, TriggerSpoken},
		{Request{From: FromMail}, KindChat, TriggerMail},
		{Request{Origin: &Origin{Channel: ch}}, KindChannel, TriggerMessage},
		{Request{Origin: &Origin{Automation: au}}, KindAutomation, TriggerAutomation},
		{Request{Origin: &Origin{Automation: au, Profile: &Profile{Name: "memory"}}}, KindJob, TriggerAutomation},
		{Request{Origin: &Origin{Subagent: sa}, From: FromLead}, KindSubagent, TriggerLead},
		{Request{Report: &Report{}}, KindWake, TriggerReport},
		{Request{Origin: &Origin{Peer: &db.BotMessage{}}, From: FromBot}, KindBot, TriggerBot},
		{Request{Origin: &Origin{Room: &db.RoomBot{}}, From: FromRoom}, KindRoom, TriggerRoom},
		// Woken by its subagents in a seat, a Bot answers the room.
		{Request{Report: &Report{}, Origin: &Origin{Room: &db.RoomBot{}}}, KindRoom, TriggerReport},
		// A lead woken on a channel answers there: it is that channel's run.
		{Request{Report: &Report{}, Origin: &Origin{Channel: ch}}, KindChannel, TriggerReport},
		{Request{Compact: true}, KindCompact, TriggerManual},
	} {
		k := c.req.Kind()
		if k.Name != c.kind || c.req.Trigger() != c.trigger {
			t.Errorf("%+v: kind %s trigger %s, want %s %s", c.req, k.Name, c.req.Trigger(), c.kind, c.trigger)
		}
		// A stored row reads back as the kind it was started as.
		if back := KindOf(k.Origin, c.req.Job()); back.Name != c.kind {
			t.Errorf("%s is stored as %q and read back as %s", c.kind, k.Origin, back.Name)
		}
	}
	// Only a job goes without the machine among the runs that work for a
	// conversation, and only a firing starts from nothing.
	for _, k := range Kinds {
		wantFresh := k.Name == KindAutomation || k.Name == KindJob
		if k.Fresh != wantFresh {
			t.Errorf("%s: fresh %v", k.Name, k.Fresh)
		}
		if k.Machine && !k.Model {
			t.Errorf("%s starts the box but asks no model", k.Name)
		}
	}
}
