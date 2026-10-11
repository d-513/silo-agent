package dummy

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"silo.agent/internal/llm"
)

// DecisionModel is the conventional decision model of the dummy provider
// ("dummy/judge"). The provider answers any model name the same way.
const DecisionModel = "judge"

var (
	decided   []llm.DecisionRequest
	completed []llm.Request
)

// Decided returns the decision requests the dummy has answered since the last
// Reset, so a test can see what a caller put in the state and asked.
func Decided() []llm.DecisionRequest {
	mu.Lock()
	defer mu.Unlock()
	return append([]llm.DecisionRequest(nil), decided...)
}

// Completed returns the non-streamed requests since the last Reset.
func Completed() []llm.Request {
	mu.Lock()
	defer mu.Unlock()
	return append([]llm.Request(nil), completed...)
}

// Decide answers typed questions from Test_* tokens in the state and the
// instructions, with no network:
//
//   - a choice picks the option named by Test_Pick_<option>, otherwise the
//     first option in name order;
//   - the noul named <q> is 0.97 with Test_Yes_<q> and 0.03 with Test_No_<q>,
//     otherwise a coin toss. The auto-approval tokens the text path answers
//     to (verdictFrom) work here too, on the gate's two questions:
//     Test_Approve is allowed and not forbidden, Test_Deny the reverse;
//   - a score is the top level with Test_Top, otherwise the bottom one.
//
// Test_Unsure makes every answer low-confidence, and Test_Decide_Error fails
// the call like an upstream that is down.
func (c *client) Decide(_ context.Context, req llm.DecisionRequest) (llm.DecisionResponse, error) {
	mu.Lock()
	decided = append(decided, req)
	mu.Unlock()
	if err := req.Validate(); err != nil {
		return llm.DecisionResponse{}, err
	}
	hay := string(req.State)
	for _, q := range req.Questions {
		hay += "\n" + q.Instructions
	}
	if strings.Contains(hay, "Test_Decide_Error") {
		return llm.DecisionResponse{}, errors.New("decide: http 529")
	}
	sure := 0.97
	if strings.Contains(hay, "Test_Unsure") {
		sure = 0.55
	}
	out := llm.DecisionResponse{
		Model:   req.Model,
		Answers: map[string]llm.Answer{},
		Usage:   llm.Usage{InputTokens: len(hay) / 4},
	}
	for name, q := range req.Questions {
		a := llm.Answer{Type: q.Type}
		switch q.Type {
		case llm.QuestionNoul:
			p := 0.5
			switch {
			case strings.Contains(hay, "Test_Yes_"+name) || approves(hay, name, true):
				p = sure
			case strings.Contains(hay, "Test_No_"+name) || approves(hay, name, false):
				p = 1 - sure
			}
			a.Noul = &p
		case llm.QuestionChoice:
			a.Choice = pick(hay, q.Options)
			conf := sure
			a.Confidence = &conf
			a.Probabilities = map[string]float64{}
			for o := range q.Options {
				a.Probabilities[o] = (1 - sure) / float64(len(q.Options)-1)
			}
			a.Probabilities[a.Choice] = sure
		case llm.QuestionScore:
			level := 0
			if strings.Contains(hay, "Test_Top") {
				level = len(q.Levels) - 1
			}
			score, conf := float64(level), sure
			a.Score, a.Confidence = &score, &conf
			a.Probabilities, a.Legend = map[string]float64{}, map[string]string{}
			for i, l := range q.Levels {
				a.Probabilities[strconv.Itoa(i)] = 0
				a.Legend[strconv.Itoa(i)] = l
			}
			a.Probabilities[strconv.Itoa(level)] = 1
		}
		out.Answers[name] = a
	}
	return out, nil
}

// approves reports whether an auto-approval token answers the gate's noul
// named q with yes (or, for yes=false, with no).
func approves(hay, q string, yes bool) bool {
	for token, allowed := range map[string]bool{"Test_Approve": true, "Test_Deny": false} {
		if !strings.Contains(hay, token) {
			continue
		}
		switch q {
		case "allowed":
			return allowed == yes
		case "forbidden":
			return allowed != yes
		}
	}
	return false
}

func pick(hay string, options map[string]string) string {
	names := make([]string, 0, len(options))
	for o := range options {
		names = append(names, o)
	}
	sort.Strings(names)
	for _, o := range names {
		if strings.Contains(hay, "Test_Pick_"+o) {
			return o
		}
	}
	return names[0]
}
