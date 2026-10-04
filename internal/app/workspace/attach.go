package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"path"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
)

// Attachment reads one workspace file as a channel attachment.
func (s *Service) Attachment(ctx context.Context, botID, rel string) (channels.Attachment, error) {
	rel = Rel(rel)
	if rel == "" {
		return channels.Attachment{}, errors.New("path required")
	}
	raw, err := s.Call(ctx, botID, &v1.Cmd{Body: &v1.Cmd_BrowseFile{
		BrowseFile: &v1.BrowseFileCmd{Path: rel, Limit: PresentLimit},
	}})
	if err != nil {
		return channels.Attachment{}, err
	}
	var row struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		return channels.Attachment{}, err
	}
	data, err := DecodeData(row.Data)
	if err != nil {
		return channels.Attachment{}, err
	}
	if len(data) == 0 {
		data = []byte(row.Content)
	}
	if len(data) == 0 {
		return channels.Attachment{}, errors.New("file is empty")
	}
	name := row.Name
	if name == "" {
		name = path.Base(rel)
	}
	mime := MIME(name)
	if mime == "" {
		mime = "application/octet-stream"
	}
	return channels.Attachment{Name: name, Mime: mime, Data: data}, nil
}
