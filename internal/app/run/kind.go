package run

// Kind is what sort of work a run is. Everything the engine does differently
// from one run to the next is a field here, so a new sort of run is a row in
// Kinds rather than another test of the Origin scattered through the loop.
type Kind struct {
	// Name is the kind as a manager reads it in the list of runs.
	Name string
	// Origin is what runs.origin holds for it. Only a job differs from its
	// Name: it is an automation's run (the rules and the approval gate read
	// it as one), told apart by runs.job.
	Origin string
	// Model says a model is asked. The others are a row written already
	// done: something that happened in a chat without the Bot answering.
	Model bool
	// Machine says the run starts the Bot's box and is what the Bot is "on"
	// (bots.last_task).
	Machine bool
	// Fresh says each run starts from its own log alone: its context, a
	// provider's session and the Python interpreter are the run's, not the
	// conversation's.
	Fresh bool
	// Titled says the run's opening names a chat that has no title yet.
	Titled bool
}

// The kinds, by Name.
const (
	KindChat       = "chat"
	KindChannel    = "channel"
	KindAutomation = "automation"
	KindJob        = "job"
	KindSubagent   = "subagent"
	KindWake       = "wake"
	KindBot        = "bot"
	KindCompact    = "compact"
	KindCommand    = "command"
	KindQuote      = "quote"
	KindVoice      = "voice"
)

// Kinds is every kind of run, in the order a list of them is filtered by.
var Kinds = []Kind{
	{Name: KindChat, Origin: "chat", Model: true, Machine: true, Titled: true},
	{Name: KindChannel, Origin: "channel", Model: true, Machine: true, Titled: true},
	{Name: KindAutomation, Origin: "automation", Model: true, Machine: true, Fresh: true, Titled: true},
	// A domain's housekeeping (Profile): its own prompt, tools and model.
	{Name: KindJob, Origin: "automation", Model: true, Fresh: true},
	{Name: KindSubagent, Origin: "subagent", Model: true, Machine: true},
	// A lead woken by its subagents' results or the Bots it messaged.
	{Name: KindWake, Origin: "subagents", Model: true, Machine: true},
	// Answering another Bot's message, in this Bot's conversation for it.
	{Name: KindBot, Origin: "bot", Model: true, Machine: true},
	// A manual compaction: a model turn follows only if a message comes in.
	{Name: KindCompact, Origin: "compact", Model: true},
	{Name: KindCommand, Origin: "command"},
	{Name: KindQuote, Origin: "quote"},
	// An exchange the call's talker answered itself, written once heard.
	{Name: KindVoice, Origin: "voice"},
}

func kind(name string) Kind {
	for _, k := range Kinds {
		if k.Name == name {
			return k
		}
	}
	return Kind{Name: name, Origin: name}
}

// Kind is the kind of run the request starts.
func (r Request) Kind() Kind {
	o := r.Origin
	if o == nil {
		o = &Origin{}
	}
	switch {
	case r.Compact:
		return kind(KindCompact)
	case o.Subagent != nil:
		return kind(KindSubagent)
	case o.Peer != nil:
		return kind(KindBot)
	case o.Profile != nil:
		return kind(KindJob)
	case o.Automation != nil:
		return kind(KindAutomation)
	case r.Report != nil && o.Channel == nil:
		return kind(KindWake)
	case o.Channel != nil:
		return kind(KindChannel)
	}
	return kind(KindChat)
}

// KindOf is the kind of a stored run, from its origin and job.
func KindOf(origin, job string) Kind {
	if job != "" {
		return kind(KindJob)
	}
	for _, k := range Kinds {
		if k.Origin == origin && k.Name != KindJob {
			return k
		}
	}
	return Kind{Name: origin, Origin: origin}
}

// What started a run (runs.trigger).
const (
	TriggerMessage    = "message"
	TriggerSpoken     = "spoken"
	TriggerMail       = "mail"
	TriggerOnboarding = "onboarding"
	TriggerLead       = "lead"
	TriggerBot        = "bot"
	TriggerReport     = "report"
	TriggerAutomation = "automation"
	TriggerManual     = "manual"
	TriggerRetry      = "retry"
)

// Trigger is what started the run the request describes.
func (r Request) Trigger() string {
	switch {
	case r.Compact:
		return TriggerManual
	case r.Resume:
		return TriggerRetry
	case r.Report != nil:
		return TriggerReport
	case r.From != "":
		return r.From
	case r.Extra.Spoken != nil:
		return TriggerSpoken
	case r.Origin != nil && r.Origin.Automation != nil:
		return TriggerAutomation
	}
	return TriggerMessage
}

// Job is the housekeeping job the run is narrowed to, "" for none.
func (r Request) Job() string {
	if r.Origin != nil && r.Origin.Profile != nil {
		return r.Origin.Profile.Name
	}
	return ""
}
