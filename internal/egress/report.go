package egress

import (
	"math"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

const (
	// blockQuiet and flagQuiet are how long the same verdict for the same
	// client, reason and host is told only once: a browser retries a blocked
	// host many times a minute, and one flag per page load is noise.
	blockQuiet = time.Minute
	flagQuiet  = 10 * time.Minute
	// maxEvents caps what waits for a session; past it the oldest go.
	maxEvents = 5000
	// beaconSamples is how many requests in a row make a rhythm.
	beaconSamples = 8
)

type quietKey struct {
	client  string
	verdict string
	reason  string
	host    string
}

type trafficKey struct {
	bot, connector, host string
	minute               int64
}

type budgetKey struct {
	client, host string
	hour         int64
}

type beaconKey struct{ client, host string }

// Reporter gathers what the filter did and hands it to the session in
// batches. Nothing in it ever blocks a connection.
type Reporter struct {
	mu      sync.Mutex
	now     func() time.Time
	events  []*v1.EgressEvent
	traffic map[trafficKey]*v1.EgressTrafficRow
	quiet   map[quietKey]time.Time
	budget  map[budgetKey]int64
	beacons map[beaconKey][]time.Time
}

func NewReporter(now func() time.Time) *Reporter {
	if now == nil {
		now = time.Now
	}
	return &Reporter{
		now:     now,
		traffic: map[trafficKey]*v1.EgressTrafficRow{},
		quiet:   map[quietKey]time.Time{},
		budget:  map[budgetKey]int64{},
		beacons: map[beaconKey][]time.Time{},
	}
}

func clientKey(c *Client) string {
	if c == nil {
		return ""
	}
	return c.Net.String()
}

// Event records a block or a flag, unless the same one was told a moment ago.
// It reports whether it was recorded.
func (r *Reporter) Event(c *Client, ev *v1.EgressEvent) bool {
	now := r.now()
	if c != nil {
		ev.BotId, ev.ConnectorId = c.BotID, c.ConnectorID
	}
	ev.At = now.UnixMilli()
	ev.Host = NormHost(ev.GetHost())
	quiet := flagQuiet
	if ev.GetVerdict() == "block" {
		quiet = blockQuiet
	}
	k := quietKey{clientKey(c), ev.GetVerdict(), ev.GetReason(), ev.GetHost()}
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.quiet[k]; ok && now.Sub(last) < quiet {
		return false
	}
	r.quiet[k] = now
	if len(r.quiet) > 50000 {
		for qk, t := range r.quiet {
			if now.Sub(t) > flagQuiet {
				delete(r.quiet, qk)
			}
		}
	}
	if len(r.events) >= maxEvents {
		r.events = r.events[1:]
	}
	r.events = append(r.events, ev)
	return true
}

// Traffic adds one request (or one connection) to the minute's sums.
func (r *Reporter) Traffic(c *Client, host string, up, down int64, verdict string) {
	if c == nil {
		return
	}
	now := r.now()
	k := trafficKey{c.BotID, c.ConnectorID, NormHost(host), now.Unix() / 60 * 60}
	r.mu.Lock()
	defer r.mu.Unlock()
	row := r.traffic[k]
	if row == nil {
		row = &v1.EgressTrafficRow{BotId: k.bot, ConnectorId: k.connector, Host: k.host, Minute: k.minute}
		r.traffic[k] = row
	}
	row.Requests++
	row.BytesUp += up
	row.BytesDown += down
	switch verdict {
	case "block":
		row.Blocked++
	case "flag":
		row.Flagged++
	}
}

// Upload counts bytes a client sent a host and reports whether this send
// took the hour's total past budget.
func (r *Reporter) Upload(c *Client, host string, n, budget int64) bool {
	if c == nil || n <= 0 || budget <= 0 {
		return false
	}
	now := r.now()
	k := budgetKey{clientKey(c), NormHost(host), now.Unix() / 3600}
	r.mu.Lock()
	defer r.mu.Unlock()
	before := r.budget[k]
	r.budget[k] = before + n
	if len(r.budget) > 20000 {
		for bk := range r.budget {
			if bk.hour < k.hour {
				delete(r.budget, bk)
			}
		}
	}
	return before <= budget && before+n > budget
}

// Beacon notes a request and reports whether the client has been calling
// host on a steady clock: the rhythm of a program reporting home, which a
// person browsing never keeps.
func (r *Reporter) Beacon(c *Client, host string) bool {
	if c == nil {
		return false
	}
	now := r.now()
	k := beaconKey{clientKey(c), NormHost(host)}
	r.mu.Lock()
	defer r.mu.Unlock()
	ts := append(r.beacons[k], now)
	if len(ts) > beaconSamples+1 {
		ts = ts[len(ts)-beaconSamples-1:]
	}
	r.beacons[k] = ts
	if len(r.beacons) > 20000 {
		for bk, v := range r.beacons {
			if now.Sub(v[len(v)-1]) > time.Hour {
				delete(r.beacons, bk)
			}
		}
	}
	if len(ts) < beaconSamples+1 {
		return false
	}
	var gaps []float64
	for i := 1; i < len(ts); i++ {
		gaps = append(gaps, ts[i].Sub(ts[i-1]).Seconds())
	}
	var mean float64
	for _, g := range gaps {
		mean += g
	}
	mean /= float64(len(gaps))
	if mean < 20 {
		return false
	}
	var v float64
	for _, g := range gaps {
		v += (g - mean) * (g - mean)
	}
	sd := math.Sqrt(v / float64(len(gaps)))
	return sd/mean < 0.1
}

// Drain takes what has gathered: the events, and the traffic sums so far.
// A minute's sum may come in several parts; the CP adds them up.
func (r *Reporter) Drain() (events []*v1.EgressEvent, rows []*v1.EgressTrafficRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events, r.events = r.events, nil
	for k, row := range r.traffic {
		rows = append(rows, row)
		delete(r.traffic, k)
	}
	return events, rows
}

// Requeue puts back what a failed send took, ahead of anything newer.
func (r *Reporter) Requeue(events []*v1.EgressEvent, rows []*v1.EgressTrafficRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(events) > 0 {
		r.events = append(events, r.events...)
		if len(r.events) > maxEvents {
			r.events = r.events[len(r.events)-maxEvents:]
		}
	}
	for _, row := range rows {
		k := trafficKey{row.GetBotId(), row.GetConnectorId(), row.GetHost(), row.GetMinute()}
		if cur := r.traffic[k]; cur != nil {
			cur.Requests += row.GetRequests()
			cur.Blocked += row.GetBlocked()
			cur.Flagged += row.GetFlagged()
			cur.BytesUp += row.GetBytesUp()
			cur.BytesDown += row.GetBytesDown()
			continue
		}
		r.traffic[k] = row
	}
}
