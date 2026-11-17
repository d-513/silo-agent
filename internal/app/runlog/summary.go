package runlog

import (
	"encoding/json"
	"sort"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

// lastErrorMax caps the error a summary quotes.
const lastErrorMax = 300

// Summary is a run in numbers. It is worked out from the run's log, so a run
// from before summaries were kept has one too.
type Summary struct {
	Turns       int            `json:"turns,omitempty"`
	In          int64          `json:"in,omitempty"`
	Out         int64          `json:"out,omitempty"`
	CacheRead   int64          `json:"cache_read,omitempty"`
	CacheWrite  int64          `json:"cache_write,omitempty"`
	SideCalls   int            `json:"side_calls,omitempty"`
	SideIn      int64          `json:"side_in,omitempty"`
	SideOut     int64          `json:"side_out,omitempty"`
	Tools       map[string]int `json:"tools,omitempty"`
	Calls       map[string]int `json:"calls,omitempty"`
	ToolErrors  int            `json:"tool_errors,omitempty"`
	Errors      int            `json:"errors,omitempty"`
	LastError   string         `json:"last_error,omitempty"`
	Approvals   map[string]int `json:"approvals,omitempty"`
	Filtered    map[string]int `json:"filtered,omitempty"`
	Compactions int            `json:"compactions,omitempty"`
	Cleared     int            `json:"cleared,omitempty"`
}

// side is the body of a SideKind event.
type side struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	In       int    `json:"in,omitempty"`
	Out      int    `json:"out,omitempty"`
	Ms       int64  `json:"ms,omitempty"`
	Verdict  string `json:"verdict,omitempty"`
	Err      string `json:"error,omitempty"`
}

// Summarize reads a run's log into its Summary.
func (s *Service) Summarize(runID string) Summary {
	var sum Summary
	// The kinds whose bodies say something: all of them short.
	var told []db.RunEvent
	s.db.Select("kind", "body").Where("run_id = ? AND kind IN ?", runID,
		[]string{"usage", "error", "filtered", SideKind}).Order("seq").Find(&told)
	for _, ev := range told {
		switch ev.Kind {
		case "usage":
			var u map[string]int64
			_ = json.Unmarshal([]byte(ev.Body), &u)
			sum.Turns++
			sum.In += u["input"]
			sum.Out += u["output"]
			sum.CacheRead += u["cache_read"]
			sum.CacheWrite += u["cache_write"]
		case "error":
			sum.Errors++
			sum.LastError = textx.CapRunes(strings.TrimSpace(ev.Body), lastErrorMax)
		case "filtered":
			var f struct {
				Verdict string `json:"verdict"`
			}
			if json.Unmarshal([]byte(ev.Body), &f) == nil && f.Verdict != "" {
				bump(&sum.Filtered, f.Verdict)
			}
		case SideKind:
			var c side
			_ = json.Unmarshal([]byte(ev.Body), &c)
			sum.SideCalls++
			sum.SideIn += int64(c.In)
			sum.SideOut += int64(c.Out)
		}
	}
	// The kinds that only count. A tool turn's calls are its "tool" events;
	// what code called is a "call" each.
	var counted []struct {
		Kind, Tool string
		N          int
	}
	s.db.Model(&db.RunEvent{}).Select("kind, tool, count(*) AS n").
		Where("run_id = ? AND kind IN ?", runID, []string{"tool", "call", "compaction", "cleared", "turn"}).
		Group("kind, tool").Scan(&counted)
	turns := 0
	for _, c := range counted {
		switch c.Kind {
		case "tool":
			if c.Tool != "" {
				add(&sum.Tools, c.Tool, c.N)
			}
		case "call":
			if c.Tool != "" {
				add(&sum.Calls, c.Tool, c.N)
			}
		case "compaction":
			sum.Compactions += c.N
		case "cleared":
			sum.Cleared += c.N
		case "turn":
			turns += c.N
		}
	}
	// A provider that reports no usage still took turns.
	if sum.Turns == 0 {
		sum.Turns = turns
	}
	var failed int64
	s.db.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ? AND body LIKE ?", runID, "tool_result", "error:%").Count(&failed)
	sum.ToolErrors = int(failed)
	var settled []struct {
		Status string
		N      int
	}
	s.db.Model(&db.Approval{}).Select("status, count(*) AS n").Where("run_id = ?", runID).Group("status").Scan(&settled)
	for _, a := range settled {
		add(&sum.Approvals, a.Status, a.N)
	}
	return sum
}

// Keep works out an ended run's summary and stores it on its row.
func (s *Service) Keep(runID string) Summary {
	sum := s.Summarize(runID)
	raw, _ := json.Marshal(sum)
	s.db.Model(&db.Run{}).Where("id = ?", runID).Update("summary", string(raw))
	return sum
}

func bump(m *map[string]int, key string) { add(m, key, 1) }

func add(m *map[string]int, key string, n int) {
	if *m == nil {
		*m = map[string]int{}
	}
	(*m)[key] += n
}

// counts is a map as the page lists it: most first, then by name.
func counts(m map[string]int) []*v1.RunCount {
	out := make([]*v1.RunCount, 0, len(m))
	for k, n := range m {
		out = append(out, &v1.RunCount{Name: k, Count: int32(n)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (sum Summary) proto() *v1.RunSummary {
	return &v1.RunSummary{
		Turns: int32(sum.Turns), Input: sum.In, Output: sum.Out, CacheRead: sum.CacheRead, CacheWrite: sum.CacheWrite,
		SideCalls: int32(sum.SideCalls), SideInput: sum.SideIn, SideOutput: sum.SideOut,
		Tools: counts(sum.Tools), Calls: counts(sum.Calls), ToolErrors: int32(sum.ToolErrors),
		Errors: int32(sum.Errors), LastError: sum.LastError,
		Approvals: counts(sum.Approvals), Filtered: counts(sum.Filtered),
		Compactions: int32(sum.Compactions), Cleared: int32(sum.Cleared),
	}
}
