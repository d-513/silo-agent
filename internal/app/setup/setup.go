// Package setup is the Bot offering its owner something to add: a library
// connector, a channel or a drive. An offer is a card in the thread, like an
// artifact's, showing the provider's mark and name, why the Bot suggests it
// and what it filled in. The owner opens Silo's own add form with those
// values filled in and submits it themselves, so the press is their consent
// and no approval is asked. They can also say not now; either way the Bot is
// told in that chat and carries on.
//
// What keeps a misled Bot from using it to point Silo somewhere: a target is
// a catalog entry (never a custom MCP URL), and the values it fills in are
// the form's own options and declared plain fields, never a secret or an
// address.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/builtin"
	"silo.agent/internal/catalog"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/drives"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

// Kind is the run event that shows an offer in a thread (body = its id);
// StateKind is the transient one its viewers get when the owner answers it.
const (
	Kind      = "setup_offer"
	StateKind = "setup_state"
)

// The kinds of thing an offer adds.
const (
	Connector = "connector"
	Channel   = "channel"
	Drive     = "drive"
)

// An offer's status.
const (
	Open    = "open"
	Done    = "done"
	Skipped = "skipped"
)

const (
	valueMax = 200
	whyMax   = 300
	// keep is how many offers a Bot keeps; older answered ones go.
	keep = 200
)

// Host is what the setup domain needs from the App.
type Host interface {
	host.Runs
	host.Emitter
	run.Engine
}

// Service owns setup offers: the tool, the thread's view of them and the
// owner's answer.
type Service struct {
	db   *gorm.DB
	host Host
}

func New(gdb *gorm.DB, h Host) *Service { return &Service{db: gdb, host: h} }

// options are the add form's own settings an offer may fill in, by kind,
// beside the provider's declared plain fields. A bool is "true" or "false".
var options = map[string]map[string]func(string) bool{
	Connector: {
		"name":              anyText,
		"expose_completion": isBool,
		"default_mode":      oneOf("allow", "ask", "deny"),
	},
	Channel: {
		"name":    anyText,
		"inbound": isBool,
		"enabled": isBool,
		"prompt":  anyText,
	},
	Drive: {
		"name":      anyText,
		"read_only": isBool,
	},
}

func anyText(string) bool  { return true }
func isBool(v string) bool { return v == "true" || v == "false" }
func oneOf(vals ...string) func(string) bool {
	return func(v string) bool {
		for _, x := range vals {
			if v == x {
				return true
			}
		}
		return false
	}
}

// addressy are field keys that name where to connect: never filled in.
var addressy = []string{"url", "host", "server", "port", "endpoint", "domain", "address_book", "base"}

func addressField(key string) bool {
	k := strings.ToLower(key)
	for _, a := range addressy {
		if strings.Contains(k, a) {
			return true
		}
	}
	return false
}

// inAddress reports whether a drive's user var is part of where rclone
// connects (R2's account_id is in its endpoint's host): a value there could
// point the drive anywhere.
func inAddress(t *drives.Template, key string) bool {
	ref := "{{user." + key
	for k, v := range t.Rclone {
		if (addressField(k) || strings.Contains(v, "://")) && strings.Contains(v, ref) {
			return true
		}
	}
	return false
}

// resolve checks an offer's target against the catalog and keeps the values
// it may fill in. It answers the catalog's name for the target.
func (s *Service) resolve(kind, target string, raw map[string]any) (label string, values map[string]string, err error) {
	plain := map[string]bool{}
	switch kind {
	case Connector:
		var c db.Connector
		s.db.Where("kind = ? AND identifier = ?", catalog.KindLibrary, target).Limit(1).Find(&c)
		if c.ID == "" {
			return "", nil, fmt.Errorf("no connector %q in the library; use a library identifier such as %s", target, s.someConnectors())
		}
		label = c.Name
		if d, ok := builtin.Lookup(c.Builtin); ok && c.Builtin != "" {
			for _, f := range d.Descriptor().Fields {
				if f.Type != builtin.FieldSecret && !addressField(f.Key) {
					plain[f.Key] = true
				}
			}
		}
	case Channel:
		ad, ok := channels.Lookup(target)
		if !ok {
			return "", nil, fmt.Errorf("no channel adapter %q; there are %s", target, strings.Join(adapters(), ", "))
		}
		d := ad.Descriptor()
		label = d.Name
		for _, f := range d.Fields {
			if f.Type != channels.FieldSecret && !addressField(f.Key) {
				plain[f.Key] = true
			}
		}
	case Drive:
		t, ok := drives.Builtin().Get(target)
		if !ok {
			return "", nil, fmt.Errorf("no drive provider %q", target)
		}
		label = t.Title
		for _, v := range t.VarsOf(drives.KindUser) {
			if !v.IsSecret() && v.Type != drives.TypeHidden && v.Type != drives.TypeURL && !addressField(v.Key) && !inAddress(t, v.Key) {
				plain[v.Key] = true
			}
		}
	default:
		return "", nil, errors.New(`kind is "connector", "channel" or "drive"`)
	}
	values = map[string]string{}
	var dropped []string
	for k, v := range raw {
		val := strings.TrimSpace(fmt.Sprint(v))
		if b, ok := v.(bool); ok {
			val = fmt.Sprint(b)
		}
		if val == "" {
			continue
		}
		if ok := options[kind][k]; ok != nil {
			if !ok(val) {
				return "", nil, fmt.Errorf("%s cannot be %q", k, val)
			}
		} else if !plain[k] {
			dropped = append(dropped, k)
			continue
		}
		values[k] = textx.ClipRunes(val, valueMax)
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		return "", nil, fmt.Errorf("these cannot be filled in: %s (secrets and addresses never are; the owner types them)", strings.Join(dropped, ", "))
	}
	return label, values, nil
}

func (s *Service) someConnectors() string {
	var names []string
	s.db.Model(&db.Connector{}).Where("kind = ? AND identifier <> ''", catalog.KindLibrary).Order("identifier").Limit(40).Pluck("identifier", &names)
	return strings.Join(names, ", ")
}

func adapters() []string {
	var out []string
	for _, d := range channels.Descriptors() {
		out = append(out, d.Slug)
	}
	sort.Strings(out)
	return out
}

// Tool is offer_setup. In a web chat the offer is a card there; the turn
// goes on, so the Bot can say what it offers. From an automation or a channel it goes
// to the main chat when the Bot may write there unasked.
func (s *Service) Tool(ctx context.Context, bot *db.Bot, runID string, args map[string]any) (string, error) {
	kind := strings.TrimSpace(toolarg.Str(args, "kind"))
	target := strings.TrimSpace(toolarg.Str(args, "target"))
	if target == "" {
		return "", errors.New("target required: a library connector identifier, a channel adapter or a drive provider")
	}
	raw, _ := args["options"].(map[string]any)
	label, values, err := s.resolve(kind, target, raw)
	if err != nil {
		return "", err
	}
	vals, _ := json.Marshal(values)
	o := db.SetupOffer{
		ID: ids.New(), BotID: bot.ID, RunID: runID, Kind: kind, Target: target, Label: label,
		Why: textx.ClipRunes(strings.TrimSpace(toolarg.Str(args, "why")), whyMax), ValuesJSON: string(vals), Status: Open,
	}
	write := func(ch *db.Chat) error {
		now := time.Now()
		o.ChatID, o.CreatedAt, o.UpdatedAt = ch.ID, now, now
		return s.db.Create(&o).Error
	}
	var ch db.Chat
	if chatID := s.host.ChatOfRun(runID); chatID != "" {
		s.db.Where("id = ?", chatID).Limit(1).Find(&ch)
	}
	if chats.Web(&ch) {
		if err := write(&ch); err != nil {
			return "", err
		}
		s.host.Emit(bot.ID, ch.ID, runID, Kind, o.ID, "")
	} else {
		// From an automation or a channel: the main chat, when the Bot may
		// write there unasked and nothing runs there.
		_, err := s.host.Place(run.Placement{BotID: bot.ID, Proactive: true, Write: func(main *db.Chat) (*db.RunEvent, error) {
			if err := write(main); err != nil {
				return nil, err
			}
			return chats.Put(s.db, main, "setup", db.RunEvent{Kind: Kind, Body: o.ID})
		}})
		if errors.Is(err, run.ErrNotMain) {
			return "", errors.New("an offer is shown in a chat; from here, tell the owner with feed or ask_owner and offer it when they are in a chat")
		}
		if err != nil {
			return "", err
		}
	}
	s.trim(bot.ID)
	out, _ := json.Marshal(map[string]any{
		"offered": label, "id": o.ID, "filled_in": values,
		"note": "shown as a card; the owner answers on it",
	})
	return string(out), nil
}

func (s *Service) trim(botID string) {
	s.db.Where("bot_id = ? AND status <> ? AND id NOT IN (?)", botID, Open,
		s.db.Model(&db.SetupOffer{}).Select("id").Where("bot_id = ?", botID).Order("created_at desc").Limit(keep),
	).Delete(&db.SetupOffer{})
}

// Proto is an offer as a thread shows it, nil when it is gone.
func (s *Service) Proto(id string) *v1.SetupOffer {
	if id == "" {
		return nil
	}
	var o db.SetupOffer
	if s.db.Where("id = ?", id).Limit(1).Find(&o); o.ID == "" {
		return nil
	}
	return proto(&o)
}

func proto(o *db.SetupOffer) *v1.SetupOffer {
	values := map[string]string{}
	_ = json.Unmarshal([]byte(o.ValuesJSON), &values)
	return &v1.SetupOffer{
		Id: o.ID, Kind: o.Kind, Target: o.Target, Label: o.Label, Why: o.Why, Options: values,
		Status: o.Status, ChatId: o.ChatID, CreatedAt: o.CreatedAt.Format(time.RFC3339),
	}
}

// ResolveSetupOffer records what the owner did with an offer (with rest,
// with every other open offer of the same turn too) and tells the Bot in its
// chat. Something set up is told at once, since the Bot can use it now; a
// "not now" waits until the turn's other offers are answered, so skipping a
// few is one message, not one each: "I set up **Email**. Not now for
// **Calendar**, **Telegram**."
func (s *Service) ResolveSetupOffer(ctx context.Context, req *connect.Request[v1.ResolveSetupOfferRequest]) (*connect.Response[v1.SetupOffer], error) {
	o, err := access.OwnBotRow[db.SetupOffer](ctx, s.db, req.Msg.GetBotId(), req.Msg.GetId(), "setup offer")
	if err != nil {
		return nil, err
	}
	outcome := req.Msg.GetOutcome()
	if outcome != Done && outcome != Skipped {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("outcome is done or skipped"))
	}
	if o.Status != Open {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this offer is answered already"))
	}
	now := time.Now()
	res := s.db.Model(&db.SetupOffer{}).Where("id = ? AND status = ?", o.ID, Open).Updates(map[string]any{"status": outcome, "updated_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("this offer is answered already"))
	}
	o.Status, o.UpdatedAt = outcome, now
	changed := []*db.SetupOffer{o}
	var rest []db.SetupOffer
	s.db.Where("bot_id = ? AND chat_id = ? AND run_id = ? AND status = ? AND id <> ?", o.BotID, o.ChatID, o.RunID, Open, o.ID).Order("created_at").Find(&rest)
	if req.Msg.GetRest() && len(rest) > 0 {
		restIDs := make([]string, len(rest))
		for i := range rest {
			restIDs[i] = rest[i].ID
			rest[i].Status, rest[i].UpdatedAt = Skipped, now
			changed = append(changed, &rest[i])
		}
		s.db.Model(&db.SetupOffer{}).Where("id IN ? AND status = ?", restIDs, Open).Updates(map[string]any{"status": Skipped, "updated_at": now})
		rest = nil
	}
	for _, c := range changed {
		s.host.Publish(c.BotID, &v1.RunEvent{ChatId: c.ChatID, Kind: StateKind, Body: c.ID, SetupOffer: proto(c)})
	}
	if outcome == Done || len(rest) == 0 {
		if err := s.tell(o, rest); err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
	}
	return connect.NewResponse(proto(o)), nil
}

// tell gives the Bot, in one message, what the owner did with the offers of
// o's turn it has not heard about yet, naming those still open. The offers
// are claimed as told in one statement, so two answers at once never tell
// the Bot the same thing twice; a message that cannot be delivered leaves
// them untold.
func (s *Service) tell(o *db.SetupOffer, open []db.SetupOffer) error {
	var news []db.SetupOffer
	s.db.Model(&news).Clauses(clause.Returning{}).
		Where("bot_id = ? AND chat_id = ? AND run_id = ? AND status <> ? AND NOT told", o.BotID, o.ChatID, o.RunID, Open).
		Update("told", true)
	if len(news) == 0 {
		return nil
	}
	sort.Slice(news, func(i, j int) bool { return news[i].CreatedAt.Before(news[j].CreatedAt) })
	var done, skipped, still []string
	ids := make([]string, 0, len(news))
	for _, n := range news {
		ids = append(ids, n.ID)
		if n.Status == Done {
			done = append(done, "**"+n.Label+"**")
		} else {
			skipped = append(skipped, "**"+n.Label+"**")
		}
	}
	for _, n := range open {
		still = append(still, "**"+n.Label+"**")
	}
	var parts []string
	if len(done) > 0 {
		parts = append(parts, "I set up "+andList(done)+".")
	}
	if len(skipped) > 0 {
		parts = append(parts, "Not now for "+strings.Join(skipped, ", ")+".")
	}
	if len(still) > 0 {
		parts = append(parts, "Still open: "+strings.Join(still, ", ")+".")
	}
	if _, err := s.host.StartOrInject(o.BotID, o.ChatID, strings.Join(parts, " "), run.Extra{}, nil); err != nil {
		s.db.Model(&db.SetupOffer{}).Where("id IN ?", ids).Update("told", false)
		return err
	}
	return nil
}

func andList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// DropChat deletes the offers shown in a deleted chat: there is nowhere to
// tell the Bot what became of them.
func (s *Service) DropChat(chatID string) {
	s.db.Where("chat_id = ?", chatID).Delete(&db.SetupOffer{})
}

// Drop deletes a Bot's offers.
func (s *Service) Drop(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.SetupOffer{})
}
