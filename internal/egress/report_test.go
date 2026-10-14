package egress

import (
	"net/netip"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// clock is a settable time for the reporter.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) add(d time.Duration)     { c.t = c.t.Add(d) }
func testClient(net, bot string) *Client { return &Client{Net: netip.MustParsePrefix(net), BotID: bot} }

func TestReporterEventQuiet(t *testing.T) {
	ck := &clock{time.Unix(1_800_000_000, 0)}
	r := NewReporter(ck.now)
	c := testClient("10.213.1.0/24", "b1")
	block := func() bool {
		return r.Event(c, &v1.EgressEvent{Verdict: "block", Reason: "threat_list", Host: "Bad.Example."})
	}
	if !block() {
		t.Fatal("the first block was not recorded")
	}
	if block() {
		t.Error("the same block a moment later was recorded again")
	}
	ck.add(blockQuiet)
	if !block() {
		t.Error("the same block after the quiet minute was not recorded")
	}
	if !r.Event(c, &v1.EgressEvent{Verdict: "flag", Reason: "exfil_sink", Host: "bad.example"}) {
		t.Error("a flag was quieted by a block")
	}
	ck.add(blockQuiet)
	if r.Event(c, &v1.EgressEvent{Verdict: "flag", Reason: "exfil_sink", Host: "bad.example"}) {
		t.Error("a flag a minute later was recorded again")
	}
	if !r.Event(testClient("10.213.2.0/24", "b2"), &v1.EgressEvent{Verdict: "block", Reason: "threat_list", Host: "bad.example"}) {
		t.Error("another client was quieted")
	}

	events, _ := r.Drain()
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	if e := events[0]; e.GetBotId() != "b1" || e.GetHost() != "bad.example" || e.GetAt() != time.Unix(1_800_000_000, 0).UnixMilli() {
		t.Errorf("event = %v", e)
	}
	if ev, _ := r.Drain(); len(ev) != 0 {
		t.Error("drain did not empty")
	}
}

func TestReporterEventCap(t *testing.T) {
	ck := &clock{time.Unix(0, 0)}
	r := NewReporter(ck.now)
	for i := 0; i < maxEvents+10; i++ {
		r.Event(nil, &v1.EgressEvent{Verdict: "flag", Reason: "x", Host: string(rune('a'+i%26)) + ".example", Detail: "n"})
		ck.add(flagQuiet)
	}
	events, _ := r.Drain()
	if len(events) != maxEvents {
		t.Errorf("events = %d, want the cap %d", len(events), maxEvents)
	}
}

func TestReporterTraffic(t *testing.T) {
	ck := &clock{time.Unix(1_800_000_030, 0)}
	r := NewReporter(ck.now)
	c := &Client{Net: netip.MustParsePrefix("10.213.2.0/24"), BotID: "b1", ConnectorID: "c1"}
	r.Traffic(c, "Example.com", 100, 2000, "")
	r.Traffic(c, "example.com:443", 50, 0, "flag")
	r.Traffic(c, "example.com", 0, 0, "block")
	r.Traffic(nil, "example.com", 1, 1, "")
	ck.add(time.Minute)
	r.Traffic(c, "example.com", 1, 1, "")

	_, rows := r.Drain()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 minutes", len(rows))
	}
	var first *v1.EgressTrafficRow
	for _, row := range rows {
		if row.GetMinute() == 1_800_000_000 {
			first = row
		}
	}
	if first == nil || first.GetRequests() != 3 || first.GetBlocked() != 1 || first.GetFlagged() != 1 || first.GetBytesUp() != 150 || first.GetBytesDown() != 2000 || first.GetConnectorId() != "c1" || first.GetHost() != "example.com" {
		t.Errorf("first minute = %v", first)
	}

	// A failed send is put back and summed with what came since.
	r.Requeue(nil, []*v1.EgressTrafficRow{first})
	r.Traffic(c, "example.com", 10, 10, "")
	ck.t = time.Unix(1_800_000_030, 0)
	r.Traffic(c, "example.com", 5, 5, "")
	_, rows = r.Drain()
	for _, row := range rows {
		if row.GetMinute() == 1_800_000_000 && (row.GetRequests() != 4 || row.GetBytesUp() != 155) {
			t.Errorf("requeued minute = %v", row)
		}
	}
}

func TestReporterRequeueEvents(t *testing.T) {
	r := NewReporter(nil)
	r.Event(nil, &v1.EgressEvent{Verdict: "block", Reason: "new", Host: "b.example"})
	r.Requeue([]*v1.EgressEvent{{Reason: "old"}}, nil)
	events, _ := r.Drain()
	if len(events) != 2 || events[0].GetReason() != "old" {
		t.Errorf("requeued events must come first: %v", events)
	}
}

func TestReporterUpload(t *testing.T) {
	ck := &clock{time.Unix(1_800_000_000, 0)}
	r := NewReporter(ck.now)
	c := testClient("10.213.1.0/24", "b1")
	if r.Upload(c, "x.example", 60, 100) {
		t.Error("under budget flagged")
	}
	if !r.Upload(c, "x.example", 60, 100) {
		t.Error("crossing the budget was not flagged")
	}
	if r.Upload(c, "x.example", 60, 100) {
		t.Error("the budget was flagged twice in an hour")
	}
	if r.Upload(c, "y.example", 60, 100) {
		t.Error("another host shares the budget")
	}
	ck.add(time.Hour)
	if r.Upload(c, "x.example", 60, 100) || !r.Upload(c, "x.example", 60, 100) {
		t.Error("a new hour did not start a new budget")
	}
	if r.Upload(nil, "x.example", 1000, 100) || r.Upload(c, "x.example", 0, 100) {
		t.Error("no client or no bytes must never flag")
	}
}

func TestReporterBeacon(t *testing.T) {
	ck := &clock{time.Unix(1_800_000_000, 0)}
	r := NewReporter(ck.now)
	c := testClient("10.213.1.0/24", "b1")
	steady := false
	for i := 0; i <= beaconSamples; i++ {
		steady = r.Beacon(c, "c2.example")
		ck.add(60*time.Second + time.Duration(i%2)*time.Second)
	}
	if !steady {
		t.Error("a steady one-minute rhythm was not seen")
	}

	// A person: irregular gaps.
	gaps := []int{3, 40, 7, 120, 30, 2, 90, 15, 60}
	for i, g := range gaps {
		if r.Beacon(c, "news.example") && i == len(gaps)-1 {
			t.Error("irregular browsing read as a beacon")
		}
		ck.add(time.Duration(g) * time.Second)
	}

	// Fast and steady is a poll loop or a download, not a beacon.
	for i := 0; i <= beaconSamples; i++ {
		if r.Beacon(c, "cdn.example") {
			t.Error("a one-second rhythm read as a beacon")
		}
		ck.add(time.Second)
	}
	if r.Beacon(nil, "c2.example") {
		t.Error("no client must never flag")
	}
}
