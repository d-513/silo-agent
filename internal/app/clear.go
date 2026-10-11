package app

import (
	"encoding/json"
	"slices"
	"unicode/utf8"

	"silo.agent/internal/llm"
)

// Clearing is the cheap step before compaction. Old tool results are most of a
// long run's context and the least needed part of it: the model already acted
// on them, and it can run a tool again. Replacing them with a one-line
// placeholder keeps every message, call and reply in place, costs no model
// call, and often frees enough that the conversation is never summarized.
//
// It rewrites the context once (the provider's cache is rebuilt from the first
// cleared result on), so it only happens when compaction would otherwise, and
// only when it frees enough to be worth that. A `cleared` event records how
// many results went, so replay clears the same ones.
const (
	clearedText = "[Old tool output cleared to save context. Run the tool again if you need it.]"
	// clearMinRunes is the size under which a result is left alone: the
	// placeholder would save nothing.
	clearMinRunes = 400
	// clearKeepResults is how many of the newest results always stay.
	clearKeepResults = 3
)

// staleResults is how many of the oldest tool results in msgs can be cleared:
// all but the newest ones, of which at least clearKeepResults and at most
// keep tokens' worth stay.
func staleResults(msgs []llm.Message, keep int) int {
	var results []int
	for i, m := range msgs {
		if m.Role == llm.RoleTool {
			results = append(results, i)
		}
	}
	n, kept, tokens := len(results), 0, 0
	for n > 0 {
		t := runeTokens(msgs[results[n-1]].Text)
		if kept >= clearKeepResults && tokens+t > keep {
			break
		}
		tokens += t
		kept++
		n--
	}
	return n
}

// clearResults replaces the first n tool results of msgs with the placeholder,
// in place, and returns the tokens that freed. Short results stay as they are.
func clearResults(msgs []llm.Message, n int) int {
	freed := 0
	for i := range msgs {
		if n <= 0 {
			break
		}
		if msgs[i].Role != llm.RoleTool {
			continue
		}
		n--
		if msgs[i].Text == clearedText || utf8.RuneCountInString(msgs[i].Text) <= clearMinRunes {
			continue
		}
		freed += runeTokens(msgs[i].Text) - runeTokens(clearedText)
		msgs[i].Text = clearedText
	}
	return freed
}

// clearStale clears the tool results a run no longer needs when that alone
// brings the conversation back under the compaction threshold. cleared is how
// many are cleared already; scale turns the engine's token estimate into the
// provider's count (used is the provider's). It returns the new history and
// count, and the tokens freed (0 when nothing was cleared and msgs is
// returned as it came).
func (a *App) clearStale(runID string, msgs []llm.Message, cleared, used, base, window int, scale float64) ([]llm.Message, int, int) {
	n := staleResults(msgs, window/10)
	if n <= cleared {
		return msgs, cleared, 0
	}
	threshold := a.cfg().Context.Threshold()
	// A copy: the previous request still holds the old slice.
	out := slices.Clone(msgs)
	freed := int(float64(clearResults(out, n)) * scale)
	if float64(freed) < threshold*float64(window)/4 || needsCompaction(used-freed, base, window, threshold) {
		return msgs, cleared, 0
	}
	meta, _ := json.Marshal(clearedMeta{Results: n})
	a.record(runID, clearedKind, "", "", string(meta))
	return out, n, freed
}
