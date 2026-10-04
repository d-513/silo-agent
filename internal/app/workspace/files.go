package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
	"silo.agent/internal/masker"
)

// Service reaches a Bot's workspace through its worker.
type Service struct {
	db   *gorm.DB
	hub  *hub.Hub
	mask func(botID string) *masker.Masker
	// onChange hears about every write or removal the human makes through the
	// Files tab, so the knowledge index can mark the folder dirty.
	onChange func(botID, path string)
}

func New(gdb *gorm.DB, h *hub.Hub, mask func(botID string) *masker.Masker, onChange func(botID, path string)) *Service {
	return &Service{db: gdb, hub: h, mask: mask, onChange: onChange}
}

func (s *Service) changed(botID, path string) {
	if s.onChange != nil {
		s.onChange(botID, path)
	}
}

// PresentLimit is the byte budget asked of the worker when a file is shown or
// sent as an attachment. The Files tab previews far less (worker browseLimit).
const PresentLimit = 32 << 20

// Call runs one command on the Bot's worker and returns its result.
func (s *Service) Call(ctx context.Context, botID string, cmd *v1.Cmd) (string, error) {
	if !s.hub.Connected(botID) {
		return "", connect.NewError(connect.CodeFailedPrecondition, hub.ErrNoWorker)
	}
	if cmd.Id == "" {
		cmd.Id = ids.New()
	}
	out, err := s.hub.Exec(ctx, botID, cmd)
	if err != nil {
		if errors.Is(err, hub.ErrNoWorker) {
			return "", connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return "", err
	}
	return out, nil
}

func (s *Service) ListFiles(ctx context.Context, req *connect.Request[v1.ListFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	raw, err := s.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: Rel(req.Msg.GetPath())}}})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Dir      bool   `json:"dir"`
		Size     int64  `json:"size"`
		Modified string `json:"modified"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := &v1.ListFilesResponse{}
	for _, r := range rows {
		out.Entries = append(out.Entries, &v1.FileEntry{
			Name: r.Name, Path: r.Path, Dir: r.Dir, Size: r.Size, Modified: r.Modified,
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ReadFile(ctx context.Context, req *connect.Request[v1.ReadFileRequest]) (*connect.Response[v1.ReadFileResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := Rel(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	raw, err := s.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: path}}})
	if err != nil {
		return nil, err
	}
	var row struct {
		Name      string `json:"name"`
		Content   string `json:"content"`
		Data      string `json:"data"`
		Binary    bool   `json:"binary"`
		Truncated bool   `json:"truncated"`
		Size      int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return nil, err
	}
	data, err := DecodeData(row.Data)
	if err != nil {
		return nil, err
	}
	content := s.mask(b.ID).Apply(row.Content)
	if !row.Binary && len(data) > 0 {
		data = []byte(s.mask(b.ID).Apply(string(data)))
	}
	return connect.NewResponse(&v1.ReadFileResponse{
		Name:      row.Name,
		Content:   content,
		Binary:    row.Binary,
		Truncated: row.Truncated,
		Size:      row.Size,
		Data:      data,
	}), nil
}

// DecodeData decodes the base64 body a browse_file reply carries.
func DecodeData(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func (s *Service) Mkdir(ctx context.Context, req *connect.Request[v1.MkdirRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := Rel(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	if _, err := s.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_Mkdir{Mkdir: &v1.MkdirCmd{Path: path}}}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}

func (s *Service) RemoveFile(ctx context.Context, req *connect.Request[v1.RemoveFileRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := Rel(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	if _, err := s.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_Remove{Remove: &v1.RemoveCmd{Path: path}}}); err != nil {
		return nil, err
	}
	s.changed(b.ID, path)
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}

// PutMax matches the worker's putLimit. Previews stay smaller (worker browseLimit).
const PutMax = 50 << 20

func (s *Service) PutFile(ctx context.Context, req *connect.Request[v1.PutFileRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := Rel(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	data := req.Msg.GetData()
	if len(data) > PutMax {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("file too large (max 50 MB)"))
	}
	if _, err := s.Call(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_PutFile{PutFile: &v1.PutFileCmd{Path: path, Data: data}}}); err != nil {
		return nil, err
	}
	s.changed(b.ID, path)
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}
