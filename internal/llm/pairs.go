package llm

import "strconv"

// missingToolResult stands in for a tool call whose result never made it into
// the history (an interrupted run, a lost event).
const missingToolResult = "error: no result recorded for this call"

// RepairToolPairs is the last line before a request leaves: every assistant
// tool call gets exactly one tool result right after it, in call order, and a
// tool result that answers no open call is dropped. OpenAI-family upstreams
// reject a history that breaks this ("No tool output found for function
// call …"), and one bad turn would otherwise poison the conversation for good.
// Call ids are made unique and non-empty. A user turn wedged between results
// (a screenshot) moves after them. The input is not modified.
func RepairToolPairs(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	used := map[string]bool{}
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role == RoleTool {
			continue // no open call to answer
		}
		if m.Role != RoleAssistant || len(m.ToolCalls) == 0 {
			out = append(out, m)
			continue
		}
		calls := make([]ToolCall, len(m.ToolCalls))
		copy(calls, m.ToolCalls)
		idx := map[string]int{}
		for j := range calls {
			id := calls[j].ID
			if id == "" {
				id = "call_" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
			}
			for base, n := id, 2; used[id]; n++ {
				id = base + "_" + strconv.Itoa(n)
			}
			used[id] = true
			if _, ok := idx[calls[j].ID]; !ok {
				idx[calls[j].ID] = j
			}
			calls[j].ID = id
		}
		m.ToolCalls = calls
		out = append(out, m)

		results := make([]*Message, len(calls))
		var wedged []Message
		k := i + 1
		for ; k < len(msgs); k++ {
			r := msgs[k]
			if r.Role == RoleUser && len(r.Images) > 0 && k+1 < len(msgs) && msgs[k+1].Role == RoleTool {
				wedged = append(wedged, r)
				continue
			}
			if r.Role != RoleTool {
				break
			}
			j, ok := idx[r.ToolCallID]
			if !ok || results[j] != nil {
				// Unknown or repeated id: give it to the first unanswered call.
				j = -1
				for n := range results {
					if results[n] == nil {
						j = n
						break
					}
				}
				if j < 0 {
					continue
				}
			}
			r.ToolCallID = calls[j].ID
			results[j] = &r
		}
		for j, r := range results {
			if r == nil {
				out = append(out, Message{Role: RoleTool, ToolCallID: calls[j].ID, Text: missingToolResult})
				continue
			}
			out = append(out, *r)
		}
		out = append(out, wedged...)
		i = k - 1
	}
	return out
}
