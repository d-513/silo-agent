package app

import (
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
)

func TestBotStatusOrder(t *testing.T) {
	row := func(st string) *v1.BotActivity { return &v1.BotActivity{State: st} }
	cases := []struct {
		machine string
		rows    []*v1.BotActivity
		needs   bool
		want    string
	}{
		{machineOnline, nil, false, "online"},
		{machineStarting, nil, false, "starting"},
		{machineStopped, nil, false, "stopped"},
		// What a person must answer and what the Bot is doing beat the box.
		{machineStopped, nil, true, "needs_you"},
		{machineStarting, []*v1.BotActivity{row("working")}, false, "working"},
		{machineOnline, []*v1.BotActivity{row("working")}, true, "needs_you"},
		{machineOnline, []*v1.BotActivity{row("working"), row("needs_you")}, false, "needs_you"},
		{machineOnline, []*v1.BotActivity{row("waiting"), row("working")}, false, "working"},
		{machineOnline, []*v1.BotActivity{row("on_call"), row("waiting")}, false, "waiting"},
		{machineStopped, []*v1.BotActivity{row("on_call")}, false, "on_call"},
	}
	for _, c := range cases {
		if got := botStatus(c.machine, c.rows, c.needs); got != c.want {
			t.Fatalf("%s %v needs=%v: got %s, want %s", c.machine, c.rows, c.needs, got, c.want)
		}
	}
}

func TestMachineState(t *testing.T) {
	if machineState(true, false) != "online" || machineState(false, true) != "starting" || machineState(false, false) != "stopped" {
		t.Fatal("the worker and the engine decide the machine")
	}
}

func TestCurrentTask(t *testing.T) {
	start := time.Now()
	before, after := start.Add(-time.Hour), start.Add(time.Minute)
	it := func(text, who string, done bool, at time.Time) db.TaskItem {
		return db.TaskItem{Text: text, Assignee: who, Done: done, CreatedAt: at}
	}
	cases := []struct {
		name  string
		items []db.TaskItem
		agent string
		task  string
		done  int32
		total int32
	}{
		{"no board", nil, "", "", 0, 0},
		{"the first not done", []db.TaskItem{it("a", "", true, after), it("b", "", false, after), it("c", "", false, after)}, "", "b", 1, 3},
		{"a board nobody touched is an older job's", []db.TaskItem{it("a", "", false, before)}, "", "", 0, 0},
		{"a task done in this run makes it this run's", []db.TaskItem{{Text: "a", Done: true, CreatedAt: before, DoneAt: &after}, it("b", "", false, before)}, "", "b", 1, 2},
		{"a lead skips what a subagent was given", []db.TaskItem{it("a", "scout", false, after), it("b", "", false, after)}, "", "b", 0, 2},
		{"a lead with nothing of its own has no task", []db.TaskItem{it("a", "scout", false, after)}, "", "", 0, 0},
		{"a subagent reads its own, from a board made before it started", []db.TaskItem{it("a", "", false, before), it("b", "Scout", false, before)}, "scout", "b", 0, 1},
		{"a subagent with all done has no task", []db.TaskItem{it("b", "scout", true, before)}, "scout", "", 0, 0},
	}
	for _, c := range cases {
		task, done, total := currentTask(c.items, c.agent, start)
		if task != c.task || done != c.done || total != c.total {
			t.Fatalf("%s: got %q %d/%d, want %q %d/%d", c.name, task, done, total, c.task, c.done, c.total)
		}
	}
}
