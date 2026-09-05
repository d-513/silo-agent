package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/hub"
	"silo.agent/internal/ids"
)

func (a *App) callWorker(ctx context.Context, botID string, cmd *v1.Cmd) (string, error) {
	if !a.Hub.Connected(botID) {
		return "", connect.NewError(connect.CodeFailedPrecondition, hub.ErrNoWorker)
	}
	if cmd.Id == "" {
		cmd.Id = ids.New()
	}
	out, err := a.Hub.Exec(ctx, botID, cmd)
	if err != nil {
		if errors.Is(err, hub.ErrNoWorker) {
			return "", connect.NewError(connect.CodeFailedPrecondition, err)
		}
		return "", err
	}
	return out, nil
}

func (a *App) ListFiles(ctx context.Context, req *connect.Request[v1.ListFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	raw, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: relWorkspace(req.Msg.GetPath())}}})
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

func (a *App) ReadFile(ctx context.Context, req *connect.Request[v1.ReadFileRequest]) (*connect.Response[v1.ReadFileResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := relWorkspace(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	raw, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{BrowseFile: &v1.BrowseFileCmd{Path: path}}})
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
	data, err := decodeFileData(row.Data)
	if err != nil {
		return nil, err
	}
	content := a.Mask(b.ID).Apply(row.Content)
	if !row.Binary && len(data) > 0 {
		data = []byte(a.Mask(b.ID).Apply(string(data)))
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

func decodeFileData(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func (a *App) Mkdir(ctx context.Context, req *connect.Request[v1.MkdirRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := relWorkspace(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	if _, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_Mkdir{Mkdir: &v1.MkdirCmd{Path: path}}}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}

func (a *App) RemoveFile(ctx context.Context, req *connect.Request[v1.RemoveFileRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := relWorkspace(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	if _, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_Remove{Remove: &v1.RemoveCmd{Path: path}}}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}

func (a *App) PutFile(ctx context.Context, req *connect.Request[v1.PutFileRequest]) (*connect.Response[v1.FileOpResponse], error) {
	b, err := a.ownBot(ctx, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	path := relWorkspace(req.Msg.GetPath())
	if path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path required"))
	}
	data := req.Msg.GetData()
	if len(data) > 2<<20 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("file too large (max 2 MB)"))
	}
	if _, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_PutFile{PutFile: &v1.PutFileCmd{Path: path, Data: data}}}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.FileOpResponse{}), nil
}
