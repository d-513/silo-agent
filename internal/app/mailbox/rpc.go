package mailbox

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
)

func (s *Service) protoBox(ctx context.Context, box *db.Mailbox) *v1.Mailbox {
	c := s.cfg()
	out := &v1.Mailbox{State: State(c), Wake: box.Wake, WakeFrom: box.WakeFrom, Keep: Keep}
	if out.State == StateOK {
		out.Address = c.MailAddress(box.Name)
		// What is wrong with the server is the operator's to see.
		if u := access.User(ctx); u != nil && u.Admin {
			out.Problem = s.Problem()
		}
	}
	return out
}

func protoMail(m *db.MailMessage, full bool) *v1.Mail {
	out := &v1.Mail{
		Id: m.ID, From: m.Sender, FromAddress: m.SenderAddr, To: m.Recipients, Subject: m.Subject,
		ReceivedAt: m.CreatedAt.Format(time.RFC3339), SentAt: textx.RFC3339(m.SentAt), Size: int32(m.Size),
		Verified: m.Verified, AuthDetail: m.AuthDetail, Read: m.ReadAt != nil, ChatId: m.ChatID,
	}
	for _, a := range attachments(m) {
		out.Attachments = append(out.Attachments, &v1.MailAttachment{Index: int32(a.Index), Name: a.Name, Type: a.Type, Size: int32(a.Size)})
	}
	if full {
		out.Text = m.Text
	} else {
		out.Preview = preview(m.Text)
	}
	return out
}

func (s *Service) ownMail(ctx context.Context, botID, id string) (*db.MailMessage, error) {
	return access.OwnBotRow[db.MailMessage](ctx, s.db, botID, id, "message")
}

// ListMail is the Bot's mailbox and what is in it, newest first. Bodies are
// left out; GetMail has them.
func (s *Service) ListMail(ctx context.Context, req *connect.Request[v1.ListMailRequest]) (*connect.Response[v1.ListMailResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	box, err := s.Box(req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	var rows []db.MailMessage
	if err := s.db.Where("bot_id = ?", box.BotID).Order("created_at desc, id").Limit(Keep).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &v1.ListMailResponse{Mailbox: s.protoBox(ctx, box)}
	for i := range rows {
		out.Messages = append(out.Messages, protoMail(&rows[i], false))
	}
	return connect.NewResponse(out), nil
}

// GetMail is one message with its body. The owner looking does not mark it
// read: read means the Bot has seen it.
func (s *Service) GetMail(ctx context.Context, req *connect.Request[v1.GetMailRequest]) (*connect.Response[v1.Mail], error) {
	m, err := s.ownMail(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(protoMail(m, true)), nil
}

func (s *Service) DeleteMail(ctx context.Context, req *connect.Request[v1.DeleteMailRequest]) (*connect.Response[v1.DeleteMailResponse], error) {
	m, err := s.ownMail(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	s.db.Where("id = ?", m.ID).Delete(&db.MailBody{})
	if err := s.db.Delete(m).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteMailResponse{}), nil
}

// UpdateMailbox sets whether, and for whom, new mail wakes the Bot.
func (s *Service) UpdateMailbox(ctx context.Context, req *connect.Request[v1.UpdateMailboxRequest]) (*connect.Response[v1.Mailbox], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	box, err := s.Box(req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	list, err := NormalizeWakeFrom(req.Msg.GetWakeFrom())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if req.Msg.GetWake() && list == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name at least one sender the Bot should wake for"))
	}
	if err := s.db.Model(box).Updates(map[string]any{"wake": req.Msg.GetWake(), "wake_from": list}).Error; err != nil {
		return nil, err
	}
	box.Wake, box.WakeFrom = req.Msg.GetWake(), list
	return connect.NewResponse(s.protoBox(ctx, box)), nil
}

// RotateMailbox replaces the Bot's address.
func (s *Service) RotateMailbox(ctx context.Context, req *connect.Request[v1.RotateMailboxRequest]) (*connect.Response[v1.Mailbox], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	if err := s.usable(); err != nil {
		return nil, err
	}
	box, err := s.Rotate(req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.protoBox(ctx, box)), nil
}

// RawPath serves a message as it arrived (.eml): /mail/raw?bot_id=…&id=….
const RawPath = "/mail/raw"

// ServeRaw downloads one message's original bytes, attachments and all, for
// the Bot's owner (cookie auth).
func (s *Service) ServeRaw(w http.ResponseWriter, r *http.Request) {
	u, err := siloauth.UserFromRequest(s.db, r)
	if err != nil {
		access.HTTPSessionError(w, err)
		return
	}
	q := r.URL.Query()
	m, err := s.ownMail(access.WithUser(r.Context(), u), strings.TrimSpace(q.Get("bot_id")), strings.TrimSpace(q.Get("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := s.raw(m.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// A stranger wrote these bytes: they are only ever a download, never a
	// page in the control plane's origin.
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", `attachment; filename="mail-`+m.ID[:8]+`.eml"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
