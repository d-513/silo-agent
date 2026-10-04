package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
) // --- taskboard ---

var assigneeRE = regexp.MustCompile(`^\s*\[([A-Za-z0-9_-]{1,24})\]\s*`)

func (a *App) boardItems(chatID string) []db.TaskItem {
	var items []db.TaskItem
	a.DB.Where("chat_id = ?", chatID).Order("n").Find(&items)
	return items
}

func formatBoard(items []db.TaskItem) string {
	var b strings.Builder
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	fmt.Fprintf(&b, "Taskboard (%d/%d done):\n", done, len(items))
	for _, it := range items {
		mark := "[ ]"
		if it.Done {
			mark = "[x]"
		}
		fmt.Fprintf(&b, "#%d %s ", it.N, mark)
		if it.Assignee != "" {
			fmt.Fprintf(&b, "[%s] ", it.Assignee)
		}
		b.WriteString(it.Text)
		if it.Done {
			b.WriteString(" — done")
			if it.DoneBy != "" {
				b.WriteString(" by " + it.DoneBy)
			}
			if it.Note != "" {
				b.WriteString(": " + it.Note)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// taskActor names who is writing to the board from a run: the subagent's name
// or "lead".
func (a *App) taskActor(runID string) (actor string, sub *db.Subagent, chatID string) {
	chatID = a.ChatOfRun(runID)
	if sa := a.subagentOfChat(chatID); sa != nil {
		return sa.Name, sa, chatID
	}
	return "lead", nil, chatID
}

// taskTool runs the four taskboard actions for chat and Python alike.
func (a *App) taskTool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any, structured bool) (string, error) {
	action := map[string]string{"task_add": "add", "task_list": "read", "task_done": "done", "task_reset": "reset"}[name]
	actor, sub, chatID := a.taskActor(runID)
	if chatID == "" {
		return "", errors.New("the taskboard needs a conversation")
	}
	board := chatID
	if sub != nil {
		board = sub.ParentChatID
	}
	if action == "reset" && sub != nil {
		return "", errors.New("only the lead can reset the taskboard")
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := a.AuthorizeAction(ctx, bot, runID, security.Tasks, action, string(argsJSON), ""); err != nil {
		return "", err
	}
	changed := false
	switch action {
	case "add":
		var texts []string
		switch v := args["tasks"].(type) {
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					texts = append(texts, s)
				}
			}
		case string:
			texts = strings.Split(v, "\n")
		}
		if s, ok := args["task"].(string); ok {
			texts = append(texts, s)
		}
		var clean []string
		for _, t := range texts {
			if t = strings.TrimSpace(t); t != "" {
				clean = append(clean, t)
			}
		}
		if len(clean) == 0 {
			return "", errors.New("tasks required")
		}
		items := a.boardItems(board)
		if len(items)+len(clean) > taskboardMax {
			return "", fmt.Errorf("the taskboard holds at most %d items; mark or reset first", taskboardMax)
		}
		next := 1
		if len(items) > 0 {
			next = items[len(items)-1].N + 1
		}
		for _, t := range clean {
			assignee := ""
			if m := assigneeRE.FindStringSubmatch(t); m != nil {
				assignee = m[1]
				t = strings.TrimSpace(t[len(m[0]):])
			}
			it := db.TaskItem{ID: ids.New(), ChatID: board, N: next, Text: textx.ClipRunes(t, taskTextMax), Assignee: assignee, CreatedBy: actor, CreatedAt: time.Now()}
			if err := a.DB.Create(&it).Error; err != nil {
				return "", err
			}
			next++
		}
		changed = true
	case "done":
		var ns []int
		switch v := args["ids"].(type) {
		case []any:
			for _, x := range v {
				switch n := x.(type) {
				case float64:
					ns = append(ns, int(n))
				case string:
					if i, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(n), "#")); err == nil {
						ns = append(ns, i)
					}
				}
			}
		}
		if n := toolarg.Int(args, "id"); n > 0 {
			ns = append(ns, n)
		}
		if len(ns) == 0 {
			return "", errors.New("ids required")
		}
		now := time.Now()
		note := textx.ClipRunes(stringArg(args, "note"), taskTextMax)
		res := a.DB.Model(&db.TaskItem{}).Where("chat_id = ? AND n IN ?", board, ns).
			Updates(map[string]any{"done": true, "done_by": actor, "note": note, "done_at": &now})
		if res.RowsAffected == 0 {
			return "", fmt.Errorf("no task numbered %v", ns)
		}
		changed = true
	case "reset":
		a.DB.Where("chat_id = ?", board).Delete(&db.TaskItem{})
		changed = true
	}
	if changed {
		a.pingLead(bot.ID, board, "board")
	}
	items := a.boardItems(board)
	if structured {
		type item struct {
			N        int    `json:"n"`
			Text     string `json:"text"`
			Assignee string `json:"assignee"`
			Done     bool   `json:"done"`
			DoneBy   string `json:"done_by"`
			Note     string `json:"note"`
		}
		out := struct {
			Items []item `json:"items"`
		}{Items: []item{}}
		for _, it := range items {
			out.Items = append(out.Items, item{it.N, it.Text, it.Assignee, it.Done, it.DoneBy, it.Note})
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	if len(items) == 0 {
		return "The taskboard is empty.", nil
	}
	return formatBoard(items), nil
}

func stringArg(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}
