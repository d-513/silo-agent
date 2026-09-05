package app

import (
	"context"
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
	raw, err := a.callWorker(ctx, b.ID, &v1.Cmd{Body: &v1.Cmd_DirList{DirList: &v1.DirListCmd{Path: req.Msg.GetPath()}}})
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
	path := req.Msg.GetPath()
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
		Binary    bool   `json:"binary"`
		Truncated bool   `json:"truncated"`
		Size      int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.ReadFileResponse{
		Name:      row.Name,
		Content:   a.Mask(b.ID).Apply(row.Content),
		Binary:    row.Binary,
		Truncated: row.Truncated,
		Size:      row.Size,
	}), nil
}
